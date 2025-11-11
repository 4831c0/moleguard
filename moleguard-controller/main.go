package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
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

type Device struct {
	Id     int    `json:"id"`
	Config string `json:"config"`
	Ip     string `json:"ip"`
}

type DeviceById struct {
	DeviceId int `json:"device_id"`
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

		if !rows.Next() {
			w.WriteHeader(http.StatusUnauthorized)

			rows.Close()
			return
		}

		rows.Close()

		next.ServeHTTP(w, r)
	})
}

func getNextFreeId(node string) (int, error) {
	rows, err := db.Query("select id from device where node = ?", node)
	if err != nil {
		return -1, err
	}

	defer rows.Close()

	hasNext := rows.Next()
	if !hasNext {
		return 1, nil
	}

	var taken []int

	for hasNext {
		var n int
		err = rows.Scan(&n)
		if err != nil {
			return -1, err
		}
		taken = append(taken, n)
		hasNext = rows.Next()
	}

	for i := 1; i <= 254; i++ {
		if !slices.Contains(taken, i) {
			return i, nil
		}
	}

	return -1, errors.New("could not find a free id")
}

var deviceMu sync.RWMutex

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
	_, err = os.Stat("chisel.json")
	if err == nil {
		mux.Handle("GET /chisel.json", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			respBytes, err := os.ReadFile("chisel.json")
			check(err)

			w.Header().Set("Content-Type", "application/json")
			w.Write(respBytes)
		})))
	}
	mux.Handle("GET /{node}/device", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceMu.RLock()
		defer deviceMu.RUnlock()

		rows, err := db.Query("select id, user_token, config, ip from device where node = ? and user_token = ?",
			r.PathValue("node"),
			r.Header.Get("Authorization"),
		)
		check(err)

		defer rows.Close()

		devices := make([]Device, 0)

		for rows.Next() {
			var device Device
			var ignored string
			check(rows.Scan(&device.Id, &ignored, &device.Config, &device.Ip))
			_ = ignored

			devices = append(devices, device)
		}

		devicesJson, err := json.Marshal(&devices)
		check(err)

		w.Header().Set("Content-Type", "application/json")
		w.Write(devicesJson)
	})))
	mux.Handle("POST /{node}/device", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceMu.Lock()
		defer deviceMu.Unlock()

		node, ok := config.Nodes[r.PathValue("node")]

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		id, err := getNextFreeId(r.PathValue("node"))
		check(err)

		req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/config?id=%d", node.Host, id), nil)
		check(err)

		req.Header.Set("Authorization", node.Token)

		resp, err := http.DefaultClient.Do(req)
		check(err)

		defer resp.Body.Close()
		respBytes, err := io.ReadAll(resp.Body)
		check(err)

		conf := strings.ReplaceAll(string(respBytes), "Endpoint = 127.0.0.1:51820", "Endpoint = "+node.TrueEndpoint)

		userToken := r.Header.Get("Authorization")

		confLines := strings.Split(conf, "\n")
		ip := "N/A"
		for _, line := range confLines {
			if strings.HasPrefix(line, "Address = ") {
				ip = strings.TrimPrefix(line, "Address = ")
				break
			}
		}

		_, err = db.Exec("insert into device values(?, ?, ?, ?, ?)", id, r.PathValue("node"), userToken, conf, ip)
		check(err)

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(fmt.Sprintf("%d", id)))
	})))
	mux.Handle("DELETE /{node}/device", authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deviceMu.Lock()
		defer deviceMu.Unlock()

		reqBytes, err := io.ReadAll(r.Body)
		check(err)

		var deviceId DeviceById
		check(json.Unmarshal(reqBytes, &deviceId))

		_, err = db.Exec("delete from device where id = ? and node = ?", deviceId.DeviceId, r.PathValue("node"))
		check(err)

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OK"))
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
