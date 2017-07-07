package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/juju/guiproxy/internal/guiconfig"
	"github.com/juju/guiproxy/internal/juju"
	"github.com/juju/guiproxy/server"
)

// version holds the guiproxy program version.
const version = "0.5.1"

var program = filepath.Base(os.Args[0])

// main starts the proxy server.
func main() {
	// Retrieve information from flags and from Juju itself (if required).
	options, err := parseOptions()
	if err != nil {
		log.Fatalf("cannot parse configuration options: %s", err)
	}
	log.Printf("%s %s\n", program, version)
	log.Println("configuring the server")
	listenAddr := ":" + strconv.Itoa(options.port)
	controllerAddr, err := juju.Info(options.controllerAddr)
	if err != nil {
		log.Fatalf("cannot retrieve Juju URLs: %s", err)
	}
	log.Printf("GUI sandbox: %s\n", options.guiURL)
	log.Printf("controller: %s\n", controllerAddr)
	if options.legacyJuju {
		log.Println("using Juju 1")
	}
	if options.envName != "" {
		log.Printf("environment: %s\n", options.envName)
	}
	if len(options.guiConfig) != 0 {
		log.Println("GUI config has been customized")
	}

	// Set up the HTTP server.
	srv := server.New(server.Params{
		ControllerAddr: controllerAddr,
		OriginAddr:     "http://0.0.0.0" + listenAddr,
		GUIURL:         options.guiURL,
		GUIConfig:      options.guiConfig,
		LegacyJuju:     options.legacyJuju,
		NoColor:        options.noColor,
	})

	// Start the GUI proxy server.
	log.Println("starting the server\n")
	log.Printf("visit the GUI at http://0.0.0.0:%d%s\n", options.port, guiconfig.BaseURL)
	if err := http.ListenAndServe(listenAddr, srv); err != nil {
		log.Fatalf("cannot start server: %s", err)
	}
}

// parseOptions returns the GUI proxy server configuration options.
func parseOptions() (*config, error) {
	flag.Usage = usage
	port := flag.Int("port", defaultPort, "GUI proxy server port")
	guiAddr := flag.String("gui", defaultGUIAddr, "address on which the GUI in sandbox mode is listening")
	controllerAddr := flag.String("controller", "", `controller address (defaults to the address of the current controller), for instance:
		-controller jimm.jujucharms.com:443`)
	guiConfig := flag.String("config", "", `override or extend fields in the GUI configuration, for instance:
		-config gisf:true
		-config 'gisf: true, charmstoreURL: "https://1.2.3.4/cs"'
		-config 'flags: {"exterminate": true}'`)

	envName := flag.String("env", "", "select a predefined environment to run against between the following:\n"+envChoices())
	legacyJuju := flag.Bool("juju1", false, "connect to a Juju 1 model")
	noColor := flag.Bool("nocolor", false, "do not use colors")
	flag.Parse()
	if !strings.HasPrefix(*guiAddr, "http") {
		*guiAddr = "http://" + *guiAddr
	}
	guiURL, err := url.Parse(*guiAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot parse GUI address: %s", err)
	}
	if *envName == "brian" {
		*envName = "qa"
	}
	overrides, err := guiconfig.ParseOverridesForEnv(*envName, *guiConfig)
	if err != nil {
		return nil, fmt.Errorf("cannot parse GUI config: %s", err)
	}
	// At this point we know that the provided environment name is valid.
	if *controllerAddr == "" && *envName != "" {
		*controllerAddr = guiconfig.Environments[*envName].ControllerAddr
	}
	return &config{
		port:           *port,
		guiURL:         guiURL,
		controllerAddr: *controllerAddr,
		envName:        *envName,
		guiConfig:      overrides,
		legacyJuju:     *legacyJuju,
		noColor:        *noColor,
	}, nil
}

const (
	defaultPort    = 8042
	defaultGUIAddr = "http://localhost:6543"
)

// config holds the GUI proxy server configuration options.
type config struct {
	port           int
	guiURL         *url.URL
	controllerAddr string
	envName        string
	guiConfig      map[string]interface{}
	legacyJuju     bool
	noColor        bool
}

// usage provides the command help and usage information.
func usage() {
	fmt.Fprintf(os.Stderr, "The %s command proxies WebSocket requests from the GUI sandbox to a Juju controller.\n", program)
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", program)
	flag.PrintDefaults()
}

// envChoices pretty formats GUI environment choices.
func envChoices() string {
	texts := make([]string, 0, len(guiconfig.Environments))
	for name := range guiconfig.Environments {
		texts = append(texts, "		- "+name)
	}
	sort.Strings(texts)
	return strings.Join(texts, "\n")
}
