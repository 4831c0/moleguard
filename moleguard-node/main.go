package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
)

var wgQuick = "/usr/bin/wg-quick"
var iptables = "/usr/sbin/iptables"

func check(err error) {
	if err != nil {
		panic(err)
	}
}

type Relay struct {
	Server string `json:"server"`
}

func ipv4ToUint32(ipStr string) (uint32, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP")
	}
	ip = ip.To4()
	if ip == nil {
		return 0, fmt.Errorf("not an IPv4 address")
	}
	return binary.BigEndian.Uint32(ip), nil
}

func uint32ToIPv4(n uint32) (string, error) {
	if n > 0xFFFFFFFF {
		return "", fmt.Errorf("number out of range")
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, n)
	return net.IP(b).String(), nil
}

func ipv6ToBigInt(ipStr string) (*big.Int, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP")
	}
	ip = ip.To16()
	if ip == nil || ip.To4() != nil {
		return nil, fmt.Errorf("not an IPv6 address")
	}
	bi := new(big.Int).SetBytes(ip)
	return bi, nil
}

func bigIntToIPv6(n *big.Int) (string, error) {
	if n.Sign() < 0 {
		return "", fmt.Errorf("negative value")
	}
	if n.BitLen() > 128 {
		return "", fmt.Errorf("number out of range")
	}
	b := n.FillBytes(make([]byte, 16))
	ip := net.IP(b)
	if ip.To4() != nil {
		return "", fmt.Errorf("constructed address is IPv4-mapped")
	}
	return ip.String(), nil
}

func run(c string, args ...string) error {
	cmd := exec.Command(c, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func downAll(confDir string) error {
	files, err := os.ReadDir(confDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		_ = exec.Command(wgQuick, "down", path.Join(confDir, file.Name())).Run()
	}

	return nil
}

func iptablesSetup(newRelay string) error {
	// Forwarding
	if err := exec.Command(iptables, "-A", "FORWARD", "-o", "eth0@if20", "!", "-d", "10.13.13.1/24", "-j", "REJECT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-A", "FORWARD", "-i", newRelay, "-j", "ACCEPT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-A", "FORWARD", "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-A", "FORWARD", "-j", "REJECT").Run(); err != nil {
		return err
	}

	// NAT

	if err := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", "eth0@if20", "-j", "MASQUERADE").Run(); err != nil {
		return err
	}
	if err := exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", newRelay, "-j", "MASQUERADE").Run(); err != nil {
		return err
	}

	return nil
}
func iptablesTeardown(oldRelay string) error {
	// Forwarding
	if err := exec.Command(iptables, "-D", "FORWARD", "-o", "eth0@if20", "!", "-d", "10.13.13.1/24", "-j", "REJECT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-D", "FORWARD", "-i", oldRelay, "-j", "ACCEPT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-D", "FORWARD", "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run(); err != nil {
		return err
	}
	if err := exec.Command(iptables, "-D", "FORWARD", "-j", "REJECT").Run(); err != nil {
		return err
	}

	// NAT

	if err := exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", "eth0@if20", "-j", "MASQUERADE").Run(); err != nil {
		return err
	}
	if err := exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", oldRelay, "-j", "MASQUERADE").Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	token := os.Getenv("TOKEN")
	// mullvadAccountNumber := os.Getenv("MULLVAD_ACCOUNT_NUMBER")
	activeRelay := os.Getenv("DEFAULT_RELAY")
	confDir := path.Join(os.Getenv("HOME"), ".config", "mullvad", "wg0")

	check(downAll(confDir))
	check(run(wgQuick, "up", path.Join(confDir, activeRelay+".conf")))
	check(iptablesSetup(activeRelay))

	i := 0
	maxI := 0
	var iMu sync.Mutex

	configFiles, err := os.ReadDir("/config")
	check(err)

	for _, file := range configFiles {
		if strings.Contains(file.Name(), "peer") {
			maxI++
		}
	}

	http.HandleFunc("GET /relay", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		check(err)

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

		log.Println("Tearing down old iptables rules")
		check(iptablesTeardown(activeRelay))
		log.Println("Disconnecting from all relays")
		check(downAll(confDir))

		activeRelay = relay.Server
		log.Printf("Switching to: %s\n", activeRelay)

		log.Println("Connecting")
		check(exec.Command(wgQuick, "up", path.Join(confDir, activeRelay+".conf")).Run())
		log.Println("Setting up iptables rules")
		check(iptablesSetup(activeRelay))
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

		iMu.Lock()
		defer iMu.Unlock()
		i++
		if i >= maxI {
			i = 1
		}

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
		w.Write([]byte(pubKey))
	})

	log.Println("Listening on http://localhost:8888")
	log.Fatal(http.ListenAndServe(":8888", nil))
}
