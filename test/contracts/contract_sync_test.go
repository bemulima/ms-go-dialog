package contracts_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestHTTPContractMatchesRegisteredRoutes(t *testing.T) {
	root := filepath.Join("..", "..")
	router := readText(t, filepath.Join(root, "internal", "transport", "http", "router.go"))
	contract := readText(t, filepath.Join(root, ".ai", "contracts", "http.yaml"))

	routePattern := regexp.MustCompile(`\b(router|api|admin|internal)\.(Get|Post|Put|Delete|Patch|Handle)\("([^"]+)"`)
	prefixes := map[string]string{"router": "", "api": "/api/v1", "admin": "/admin/v1", "internal": "/internal/v1"}
	registered := make(map[string]struct{})
	for _, match := range routePattern.FindAllStringSubmatch(router, -1) {
		method := strings.ToUpper(match[2])
		if method == "HANDLE" {
			method = "GET"
		}
		registered[method+" "+prefixes[match[1]]+match[3]] = struct{}{}
	}

	documented := make(map[string]struct{})
	prefix := ""
	for scanner := bufio.NewScanner(strings.NewReader(contract)); scanner.Scan(); {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "- prefix: ") {
			prefix = strings.TrimSpace(strings.TrimPrefix(line, "- prefix: "))
			if prefix == "/" {
				prefix = ""
			}
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(line, "- "), " ", 2)
		if len(parts) == 2 && isHTTPMethod(parts[0]) {
			documented[parts[0]+" "+prefix+parts[1]] = struct{}{}
		}
	}

	assertSameSet(t, "HTTP routes", registered, documented)
}

func TestWebSocketContractMatchesProtocolEvents(t *testing.T) {
	root := filepath.Join("..", "..")
	protocol := readText(t, filepath.Join(root, "internal", "transport", "websocket", "protocol.go"))
	contract := readText(t, filepath.Join(root, ".ai", "contracts", "websocket.yaml"))

	mapStart := strings.Index(protocol, "var websocketTypes")
	if mapStart < 0 {
		t.Fatal("websocketTypes declaration was not found")
	}
	mapEnd := strings.Index(protocol[mapStart:], "\n\n")
	if mapEnd < 0 {
		t.Fatal("websocketTypes declaration was not found")
	}
	eventPattern := regexp.MustCompile(`:\s*"([^"]+)"`)
	implemented := make(map[string]struct{})
	for _, match := range eventPattern.FindAllStringSubmatch(protocol[mapStart:mapStart+mapEnd], -1) {
		implemented[match[1]] = struct{}{}
	}

	documented := make(map[string]struct{})
	inDurable := false
	for scanner := bufio.NewScanner(strings.NewReader(contract)); scanner.Scan(); {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "durable:" {
			inDurable = true
			continue
		}
		if inDurable && strings.HasPrefix(line, "    ephemeral:") {
			break
		}
		if inDurable && strings.HasPrefix(trimmed, "- ") {
			documented[strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))] = struct{}{}
		}
	}

	assertSameSet(t, "WebSocket durable events", implemented, documented)
}

func readText(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func isHTTPMethod(value string) bool {
	switch value {
	case "GET", "POST", "PUT", "DELETE", "PATCH":
		return true
	default:
		return false
	}
}

func assertSameSet(t *testing.T, name string, actual, expected map[string]struct{}) {
	t.Helper()
	missing, extra := setDifference(actual, expected), setDifference(expected, actual)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("%s contract drift: undocumented=%v not_registered=%v", name, missing, extra)
	}
}

func setDifference(left, right map[string]struct{}) []string {
	result := make([]string, 0)
	for value := range left {
		if _, exists := right[value]; !exists {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
