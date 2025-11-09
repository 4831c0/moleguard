package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type NodeConfig struct {
	Host         string `json:"host"`
	TrueEndpoint string `json:"true_endpoint"`
	Token        string `json:"token"`
}

type Config struct {
	Nodes map[string]NodeConfig `json:"nodes"`
}

type LoginReq struct {
	Token string `json:"token"`
}

type Resp struct {
	Data    any    `json:"data"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type MullvadRelay struct {
	Hostname             string        `json:"hostname"`
	CountryCode          string        `json:"country_code"`
	CountryName          string        `json:"country_name"`
	CityCode             string        `json:"city_code"`
	CityName             string        `json:"city_name"`
	Fqdn                 string        `json:"fqdn"`
	Active               bool          `json:"active"`
	Owned                bool          `json:"owned"`
	Provider             string        `json:"provider"`
	Ipv4AddrIn           string        `json:"ipv4_addr_in"`
	Ipv6AddrIn           *string       `json:"ipv6_addr_in"`
	NetworkPortSpeed     int           `json:"network_port_speed"`
	Stboot               bool          `json:"stboot"`
	Type                 string        `json:"type"`
	StatusMessages       []interface{} `json:"status_messages"`
	Pubkey               string        `json:"pubkey,omitempty"`
	MultihopPort         int           `json:"multihop_port,omitempty"`
	SocksName            *string       `json:"socks_name,omitempty"`
	SocksPort            int           `json:"socks_port,omitempty"`
	Daita                bool          `json:"daita,omitempty"`
	Ipv4V2Ray            string        `json:"ipv4_v2ray,omitempty"`
	SshFingerprintSha256 string        `json:"ssh_fingerprint_sha256,omitempty"`
	SshFingerprintMd5    string        `json:"ssh_fingerprint_md5,omitempty"`
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

var db = initDB()

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")

		if token == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		rows, err := db.Query("SELECT * FROM users WHERE token = ?", token)
		check(err)

		defer rows.Close()

		if !rows.Next() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	var config Config
	configBytes, err := os.ReadFile("config.json")
	check(err)

	var mullvadRelays []MullvadRelay

	check(json.Unmarshal(configBytes, &config))

	mux := http.NewServeMux()

	// un-auth
	mux.Handle("POST /check", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		check(err)

		var login LoginReq
		check(json.Unmarshal(bodyBytes, &login))

		rows, err := db.Query("SELECT * FROM users WHERE token = ?", login.Token)
		check(err)

		defer rows.Close()

		if !rows.Next() {
			respBytes, err := json.Marshal(&Resp{
				Data:    nil,
				Success: false,
				Error:   "Go away",
			})
			check(err)

			w.Header().Set("Content-Type", "application/json")
			w.Write(respBytes)
			return
		}

		respBytes, err := json.Marshal(&Resp{
			Data:    nil,
			Success: true,
			Error:   "",
		})
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	}))

	// auth
	mux.Handle("GET /nodes", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var keys []string
		for k := range config.Nodes {
			keys = append(keys, k)
		}

		kBytes, err := json.Marshal(&keys)
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(kBytes)
	})))
	mux.Handle("GET /relays", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(mullvadRelays) == 0 {
			resp, err := http.Get("https://api.mullvad.net/www/relays/all/")
			check(err)

			defer resp.Body.Close()

			respBytes, err := io.ReadAll(resp.Body)
			check(err)

			var relays []MullvadRelay

			check(json.Unmarshal(respBytes, &relays))
			mullvadRelays = relays
		}

		var hosts []string
		for _, relay := range mullvadRelays {
			hosts = append(hosts, relay.Hostname)
		}

		hostsBytes, err := json.Marshal(&hosts)
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(hostsBytes)
	})))
	mux.Handle("GET /{node}/pk", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := config.Nodes[r.PathValue("node")]

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		r, err := http.NewRequest("GET", "http://"+node.Host+"/pk", nil)
		check(err)

		r.Header.Set("Authorization", node.Token)

		resp, err := http.DefaultClient.Do(r)
		check(err)

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		check(err)

		w.Header().Set("Content-Type", "text/plain")
		w.Write(respBytes)
	})))
	mux.Handle("GET /{node}/config", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := config.Nodes[r.PathValue("node")]

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r, err := http.NewRequest("GET", "http://"+node.Host+"/config", nil)
		check(err)

		r.Header.Set("Authorization", node.Token)

		resp, err := http.DefaultClient.Do(r)
		check(err)

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		check(err)

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(strings.ReplaceAll(string(respBytes), "Endpoint = 127.0.0.1:51820", "Endpoint = "+node.TrueEndpoint)))
	})))
	mux.Handle("GET /{node}/relay", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := config.Nodes[r.PathValue("node")]

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		r, err := http.NewRequest("GET", "http://"+node.Host+"/relay", nil)
		check(err)

		r.Header.Set("Authorization", node.Token)

		resp, err := http.DefaultClient.Do(r)
		check(err)

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	})))
	mux.Handle("POST /{node}/relay", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := config.Nodes[r.PathValue("node")]

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reqBody, err := io.ReadAll(r.Body)
		check(err)

		r, err = http.NewRequest("POST", "http://"+node.Host+"/relay", nil)
		check(err)

		r.Header.Set("Authorization", node.Token)
		r.Body = io.NopCloser(bytes.NewReader(reqBody))

		resp, err := http.DefaultClient.Do(r)
		check(err)

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(respBytes)
	})))

	mux.Handle("/private/static/", authMiddleware(http.StripPrefix("/private/static", http.FileServer(http.Dir("./private")))))
	mux.Handle("/", http.FileServer(http.Dir("./static")))

	log.Println("Listening on http://127.0.0.1:6128")
	log.Fatal(http.ListenAndServe(":6128", mux))
}
