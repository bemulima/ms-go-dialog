package filescan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
)

type ClamAV struct {
	Address string
	Timeout time.Duration
}

func (c ClamAV) Scan(ctx context.Context, data []byte) error {
	if strings.TrimSpace(c.Address) == "" {
		return domain.ErrFileScanUnavailable
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))
	if _, err = connection.Write([]byte("zINSTREAM\x00")); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
	}
	for reader := bytes.NewReader(data); reader.Len() > 0; {
		size := min(reader.Len(), 64<<10)
		chunk := make([]byte, size)
		if _, err = io.ReadFull(reader, chunk); err != nil {
			return err
		}
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(size))
		if _, err = connection.Write(header[:]); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
		}
		if _, err = connection.Write(chunk); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
		}
	}
	if _, err = connection.Write([]byte{0, 0, 0, 0}); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
	}
	response, err := bufio.NewReader(connection).ReadString(0)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrFileScanUnavailable, err)
	}
	switch {
	case strings.Contains(response, " FOUND"):
		return domain.ErrFileInfected
	case strings.Contains(response, " OK"):
		return nil
	default:
		return fmt.Errorf("%w: unexpected ClamAV response", domain.ErrFileScanUnavailable)
	}
}

var _ attachmentuc.FileScanner = (*ClamAV)(nil)
