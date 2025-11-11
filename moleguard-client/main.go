package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/4831c0/moleguard/common"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

var sockClient http.Client

func init() {
	conn, err := net.Dial("unix", common.MoleguardSock)
	check(err)

	sockClient = http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return conn, nil
			},
		},
	}
}

func updateConfig(state *common.State) error {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", "http://unix/state", io.NopCloser(bytes.NewReader(stateBytes)))
	if err != nil {
		return err
	}
	resp, err := sockClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(respBytes, &state)
}

func wgQuick(mode string, node string) error {
	stateBytes := []byte(node)

	req, err := http.NewRequest("POST", fmt.Sprintf("http://unix/wg-quick-%s", mode), io.NopCloser(bytes.NewReader(stateBytes)))
	if err != nil {
		return err
	}
	resp, err := sockClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	return err
}

func getBytes(path string) ([]byte, error) {
	resp, err := sockClient.Get("http://unix" + path)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func main() {
	var fReset bool
	var fNode string

	flag.BoolVar(&fReset, "reset", false, "reset all state")
	flag.StringVar(&fNode, "node", "", "node to connect to")

	flag.Parse()

	var state common.State

	if fReset {
		fmt.Println("Resetting config")
		check(updateConfig(&state))
		fmt.Println("Done")
		os.Exit(0)
	}

	respBytes, err := getBytes("/state")
	check(err)

	check(json.Unmarshal(respBytes, &state))

	reader := bufio.NewReader(os.Stdin)
	confUpdate := false
	for len(state.IP) == 0 {
		fmt.Print("IP of server: ")
		confUpdate = true
		state.IP, _ = reader.ReadString('\n')
		state.IP = strings.TrimSpace(state.IP)

		_, err := common.IPv4ToUint32(state.IP)

		if err != nil {
			state.IP = ""
		}
	}
	for len(state.VpnHost) == 0 {
		fmt.Print("Hostname of vpn panel (ex.: vpn.example.com): ")
		confUpdate = true
		state.VpnHost, _ = reader.ReadString('\n')
		state.VpnHost = strings.TrimSpace(state.VpnHost)

		if strings.Contains(state.VpnHost, " ") {
			state.VpnHost = ""
		}
	}
	for len(state.FrontHost) == 0 {
		fmt.Print("Hostname of fronting website (ex.: example.com): ")
		confUpdate = true
		state.FrontHost, _ = reader.ReadString('\n')
		state.FrontHost = strings.TrimSpace(state.FrontHost)

		if strings.Contains(state.FrontHost, " ") {
			state.FrontHost = ""
		}
	}
	for len(state.Token) == 0 {
		fmt.Print("User token: ")
		confUpdate = true
		state.Token, _ = reader.ReadString('\n')
		state.Token = strings.TrimSpace(state.Token)

		if strings.Contains(state.Token, " ") {
			state.Token = ""
		}
	}

	if confUpdate {
		fmt.Println("Updating config")
		check(updateConfig(&state))
		confUpdate = false
	}

	var nodes []string

	respBytes, err = getBytes("/nodes")
	check(err)

	check(json.Unmarshal(respBytes, &nodes))

	if state.Slots == nil {
		state.Slots = make(map[string]int)
	}

	for _, node := range nodes {
		_, ok := state.Slots[node]

		if !ok {
			fmt.Printf("Device id for %s: ", node)

			str, _ := reader.ReadString('\n')
			n, err := strconv.Atoi(strings.TrimSpace(str))
			check(err)
			state.Slots[node] = n
			confUpdate = true
		}
	}

	if confUpdate {
		fmt.Println("Updating config")
		check(updateConfig(&state))
		confUpdate = false
	}

	respBytes, err = getBytes("/sync-conf")
	check(err)

	if string(respBytes) != "ok" {
		panic("failed to sync configs: " + string(respBytes))
	}

	wgStateB, err := getBytes("/wg")
	check(err)
	wgState := string(wgStateB)

	fmt.Println("Nodes:")
	activeNode := ""
	for _, node := range nodes {
		fmt.Print("- ")
		fmt.Print(node)

		if strings.Contains(wgState, "interface: wg-"+node+"\n") {
			fmt.Print(" [active]")
			activeNode = node
		}
		fmt.Println()
	}

	if activeNode != "" {
		fmt.Printf("Disconnecting from %s\n", activeNode)
		check(wgQuick("down", path.Join(common.MoleguardWgConfActive, "wg-"+activeNode+".conf")))
	}
	if fNode != "" {
		fmt.Printf("Connecting to %s\n", fNode)
		check(wgQuick("up", path.Join(common.MoleguardWgConfActive, "wg-"+fNode+".conf")))

		state.LastNode = fNode
		_ = updateConfig(&state)
	}
}
