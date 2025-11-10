package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

var wgQuick = "/usr/bin/wg-quick"
var iptables = "/usr/sbin/iptables"
var mullvadUpgradeTunnel string

func init() {
	wd, err := os.Getwd()
	check(err)

	mullvadUpgradeTunnel = path.Join(wd, "mullvad-upgrade-tunnel")
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

type Relay struct {
	Server string `json:"server"`
}

func run(c string, args ...string) error {
	cmd := exec.Command(c, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func netCheck() bool {
	conn, err := net.Dial("tcp", "1.1.1.1:443")
	if err == nil {
		conn.Close()
		return true
	}

	conn, err = net.Dial("tcp", "google.com:443")
	if err == nil {
		conn.Close()
		return true
	}

	conn, err = net.Dial("tcp", "github.com:443")
	if err == nil {
		conn.Close()
		return true
	}

	conn, err = net.Dial("tcp", "cloudflare.com:443")
	if err == nil {
		conn.Close()
		return true
	}

	return false
}

func main() {
	token := os.Getenv("TOKEN")
	defaultRelay := os.Getenv("DEFAULT_RELAY")
	confDir := path.Join(os.Getenv("HOME"), ".config", "mullvad", "wg0")

	activeRelay = defaultRelay

	check(downAll(confDir))
	check(run(wgQuick, "up", path.Join(confDir, activeRelay+".conf")))
	check(run(mullvadUpgradeTunnel, "-wg-interface", activeRelay))
	check(iptablesSetup(activeRelay))

	go func() {
		for {
			time.Sleep(5 * time.Second)

			if !netCheck() {
				log.Println("Failed to reach internet")
				log.Println("Reconnecting to mullvad")
				check(mullvadChange(activeRelay, confDir))
				time.Sleep(10 * time.Second)
			}
		}
	}()

	http.HandleFunc("GET /relay", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		jsonBytes, err := json.Marshal(Relay{Server: activeRelay})
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	})

	http.HandleFunc("POST /relay", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(r.Body)
		check(err)

		var relay Relay
		check(json.Unmarshal(body, &relay))

		log.Printf("Switching to: %s\n", activeRelay)
		check(mullvadChange(relay.Server, confDir))
		log.Println("Done")

		jsonBytes, err := json.Marshal(Relay{Server: relay.Server})
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBytes)
	})

	http.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		q := r.URL.Query()
		i, err := strconv.Atoi(q.Get("id"))
		check(err)

		confBytes, err := os.ReadFile(fmt.Sprintf("/config/peer%d/peer%d.conf", i, i))
		check(err)

		confLines := strings.Split(string(confBytes), "\n")

		confB := strings.Builder{}
		for _, line := range confLines {
			if strings.HasPrefix(line, "ListenPort = ") {
				continue
			}

			confB.WriteString(line)
			confB.WriteRune('\n')
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(confB.String()))
	})

	http.HandleFunc("GET /pk", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		pubKey, err := os.ReadFile("/config/server/publickey-server")
		check(err)

		w.Header().Set("Content-Type", "text/plain")
		w.Write(pubKey)
	})

	log.Println("Listening on http://localhost:8888")
	log.Fatal(http.ListenAndServe(":8888", nil))
}
