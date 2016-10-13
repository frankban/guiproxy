package server_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	it "github.com/frankban/guiproxy/internal/testing"
	"github.com/frankban/guiproxy/server"
)

func TestNew(t *testing.T) {
	// Set up test servers.
	gui := httptest.NewServer(newGUIServer())
	defer gui.Close()

	juju := httptest.NewTLSServer(newJujuServer())
	defer juju.Close()

	jujuURL := it.MustParseURL(t, juju.URL)
	jujuParts := strings.Split(jujuURL.Host, ":")
	ts := httptest.NewServer(server.New(server.Params{
		ControllerAddr: jujuURL.Host,
		ModelUUID:      "example-uuid",
		OriginAddr:     "http://1.2.3.4:4242",
		Port:           4242,
		GUIURL:         it.MustParseURL(t, gui.URL),
	}))
	defer ts.Close()

	serverURL := it.MustParseURL(t, ts.URL)
	controllerPath := fmt.Sprintf("/controller/%s/%s/controller-api", jujuParts[0], jujuParts[1])
	modelPath1 := fmt.Sprintf("/model/%s/%s/uuid/model-api", jujuParts[0], jujuParts[1])
	modelPath2 := fmt.Sprintf("/model/%s/%s/another-uuid/model-api", jujuParts[0], jujuParts[1])

	t.Run("testJujuWebSocketController", testJujuWebSocket(serverURL, "/api", controllerPath))
	t.Run("testJujuWebSocketModel1", testJujuWebSocket(serverURL, "/model/uuid/api", modelPath1))
	t.Run("testJujuWebSocketModel2", testJujuWebSocket(serverURL, "/model/another-uuid/api", modelPath2))
	t.Run("testJujuHTTPS", testJujuHTTPS(serverURL))
	t.Run("testGUIConfig", testGUIConfig(serverURL, jujuURL))
	t.Run("testGUIStaticFiles", testGUIStaticFiles(serverURL))
}

func testJujuWebSocket(serverURL *url.URL, dstPath, srcPath string) func(t *testing.T) {
	origin := "http://localhost/"
	u := *serverURL
	u.Scheme = "ws"
	socketURL := u.String() + srcPath
	return func(t *testing.T) {
		// Connect to the remote WebSocket.
		ws, err := websocket.Dial(socketURL, "", origin)
		it.AssertError(t, err, nil)
		defer ws.Close()
		// Send a message.
		msg := jsonMessage{
			Request: "my api request",
		}
		err = websocket.JSON.Send(ws, msg)
		it.AssertError(t, err, nil)
		// Retrieve the response from the WebSocket server.
		err = websocket.JSON.Receive(ws, &msg)
		it.AssertError(t, err, nil)
		it.AssertString(t, msg.Request, "my api request")
		it.AssertString(t, msg.Response, dstPath)
	}
}

func testJujuHTTPS(serverURL *url.URL) func(t *testing.T) {
	return func(t *testing.T) {
		// Make the HTTP request to retrieve a Juju HTTPS API endpoint.
		resp, err := http.Get(serverURL.String() + "/juju-core/api/path")
		it.AssertError(t, err, nil)
		defer resp.Body.Close()
		// The request succeeded.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("invalid response code from Juju endpoint: %v", resp.StatusCode)
		}
		// The response body includes the expected content.
		b, err := ioutil.ReadAll(resp.Body)
		it.AssertError(t, err, nil)
		it.AssertString(t, string(b), "juju: /api/path")
	}
}

func testGUIConfig(serverURL, jujuURL *url.URL) func(t *testing.T) {
	return func(t *testing.T) {
		// Make the HTTP request to retrieve the GUI configuration file.
		resp, err := http.Get(serverURL.String() + "/config.js")
		it.AssertError(t, err, nil)
		defer resp.Body.Close()
		// The request succeeded.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("invalid response code from config.js: %v", resp.StatusCode)
		}
		// The response body includes the GUI configuration.
		var expected bytes.Buffer
		err = server.ConfigTemplate.Execute(&expected, map[string]interface{}{
			"addr": jujuURL.Host,
			"port": 4242,
			"uuid": "example-uuid",
		})
		it.AssertError(t, err, nil)
		b, err := ioutil.ReadAll(resp.Body)
		it.AssertError(t, err, nil)
		it.AssertString(t, string(b), expected.String())
	}
}

func testGUIStaticFiles(serverURL *url.URL) func(t *testing.T) {
	return func(t *testing.T) {
		// Make the HTTP request to retrieve a GUI static file.
		resp, err := http.Get(serverURL.String() + "/my/path")
		it.AssertError(t, err, nil)
		defer resp.Body.Close()
		// The request succeeded.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("invalid response code from GUI static file: %v", resp.StatusCode)
		}
		// The response body includes the expected content.
		b, err := ioutil.ReadAll(resp.Body)
		it.AssertError(t, err, nil)
		it.AssertString(t, string(b), "gui: /my/path")
	}
}

// newGUIServer creates and returns a new test server simulating a remote Juju
// GUI run in sandbox mode.
func newGUIServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "gui: "+req.URL.Path)
	})
	return mux
}

// newTestServer creates and returns a new test server simulating a remote Juju
// controller and model.
func newJujuServer() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api", websocket.Handler(echoHandler))
	mux.Handle("/model/", websocket.Handler(echoHandler))
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "juju: "+req.URL.Path)
	})
	return mux
}

// echoHandler is a WebSocket handler repeating what it receives.
func echoHandler(ws *websocket.Conn) {
	path := ws.Request().URL.Path
	var msg jsonMessage
	var err error
	for {
		err = websocket.JSON.Receive(ws, &msg)
		if err == io.EOF {
			return
		}
		if err != nil {
			panic(err)
		}
		msg.Response = path
		if err = websocket.JSON.Send(ws, msg); err != nil {
			panic(err)
		}
	}
}

// jsonMessage holds messages used for testing the WebSocket handlers.
type jsonMessage struct {
	Request  string
	Response string
}