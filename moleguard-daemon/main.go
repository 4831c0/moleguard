package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/4831c0/moleguard/common"
	"github.com/gin-gonic/gin"
	chclient "github.com/jpillora/chisel/client"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

var chiselActive = atomic.Bool{}

func runChisel() {
	config := chclient.Config{Headers: http.Header{}}

	chiselBytes, err := os.ReadFile(common.MoleguardChisel)
	check(err)

	var chiselConfig common.ChiselConfig
	check(json.Unmarshal(chiselBytes, &chiselConfig))

	var remotes []string
	for i := 0; i < chiselConfig.Nodes; i++ {
		port := 51820 + i
		remotes = append(remotes, fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d/udp", port, port))
	}

	config.Server = "https://" + chiselConfig.IP
	config.Headers.Set("Host", chiselConfig.Front)
	config.TLS.ServerName = chiselConfig.Front
	config.Remotes = remotes
	config.Auth = fmt.Sprintf("%s:%s", chiselConfig.Username, chiselConfig.Password)

	client, err := chclient.NewClient(&config)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		err = client.Run()
		fmt.Println(err)
		time.Sleep(time.Second * 5)
	}
}

func cleanup() {
	log.Println("Removing moleguard socket")
	_ = os.Remove(common.MoleguardSock)
}

func initChisel(state common.State) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/chisel.json", state.VpnHost), nil)
	if err != nil {
		if exists(common.MoleguardChisel) {
			return
		} else {
			panic(err)
		}
	}

	req.Header.Set("Authorization", state.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if exists(common.MoleguardChisel) {
			return
		} else {
			panic(err)
		}
	}

	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		if exists(common.MoleguardChisel) {
			return
		} else {
			panic(err)
		}
	}

	check(os.WriteFile(common.MoleguardChisel, respBytes, 0600))
}

func main() {
	var state common.State

	http.DefaultTransport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == state.VpnHost+":443" {
			return net.Dial(network, state.IP+":443")
		}
		if addr == state.FrontHost+":443" {
			return net.Dial(network, state.IP+":443")
		}

		return nil, errors.New("unable to resolve domain: " + addr)
	}

	if exists(common.MoleguardState) {
		stateBytes, err := os.ReadFile(common.MoleguardState)
		check(err)
		check(json.Unmarshal(stateBytes, &state))
	}

	{
		wg, err := exec.LookPath("wg")
		check(err)
		wgQuick, err := exec.LookPath("wg-quick")
		check(err)

		wgBytes, err := exec.Command(wg).Output()
		check(err)

		wgTxt := string(wgBytes)

		if state.LastNode != "" && !strings.Contains(wgTxt, "interface wg-node-") {
			if !chiselActive.Load() {
				chiselActive.Store(true)
				go runChisel()
			}

			log.Printf("Starting last used wg config: %s\n", state.LastNode)
			_ = exec.Command(wgQuick, "up", path.Join(common.MoleguardWgConfActive, "wg-"+state.LastNode+".conf")).Run()
		}
	}

	defer cleanup()

	if !exists(common.MoleguardDir) {
		check(os.MkdirAll(common.MoleguardDir, 0711))
	}
	if !exists(common.MoleguardWgConfDir) {
		check(os.MkdirAll(common.MoleguardWgConfDir, 0700))
	}
	if !exists(common.MoleguardWgConfActive) {
		check(os.MkdirAll(common.MoleguardWgConfActive, 0700))
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	router.GET("/wg", func(c *gin.Context) {
		wg, err := exec.LookPath("wg")
		check(err)

		wgOut, err := exec.Command(wg).Output()
		check(err)

		c.String(200, string(wgOut))
	})

	router.POST("/wg-quick-up", func(c *gin.Context) {
		var intf string

		check(c.BindPlain(&intf))

		wgQuick, err := exec.LookPath("wg-quick")
		check(err)

		log.Println("wg-quick up " + intf)
		wgOut, _ := exec.Command(wgQuick, "up", intf).Output()

		c.String(200, string(wgOut))
	})

	router.POST("/wg-quick-down", func(c *gin.Context) {
		var intf string

		check(c.BindPlain(&intf))

		wgQuick, err := exec.LookPath("wg-quick")
		check(err)

		log.Println("wg-quick down " + intf)
		wgOut, _ := exec.Command(wgQuick, "down", intf).Output()

		c.String(200, string(wgOut))
	})

	router.GET("/state", func(c *gin.Context) {
		c.JSON(200, &state)
	})

	router.GET("/sync-conf", func(c *gin.Context) {
		initChisel(state)

		if !chiselActive.Load() {
			chiselActive.Store(true)
			go runChisel()
		}

		for _, node := range state.NodeCache {
			selectedId, ok := state.Slots[node]

			if !ok {
				c.String(500, fmt.Sprintf("no slot selected for node: %s", node))
				return
			}
			confPath := path.Join(common.MoleguardWgConfDir, "wg-"+node+".conf")
			confModPath := path.Join(common.MoleguardWgConfActive, "wg-"+node+".conf")
			req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/%s/device", state.VpnHost, node), nil)
			if err != nil {
				if exists(confModPath) {
					continue
				} else {
					panic(err)
				}
			}

			req.Header.Set("Authorization", state.Token)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				if exists(confModPath) {
					continue
				} else {
					panic(err)
				}
			}

			respBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				if exists(confModPath) {
					continue
				} else {
					panic(err)
				}
			}

			resp.Body.Close()

			var devices []common.Device
			err = json.Unmarshal(respBytes, &devices)

			var device *common.Device = nil

			for _, d := range devices {
				if d.Id == selectedId {
					device = &d
					break
				}
			}

			if device == nil {
				c.String(500, fmt.Sprintf("slot %d can't be found for node: %s", selectedId, node))
				return
			}

			err = os.WriteFile(confPath, []byte(device.Config), 0600)
			check(err)

			confStr := device.Config

			ipInt, err := common.IPv4ToUint32(state.IP)
			check(err)

			cidrPairs := common.BuildCIDRsExcept(ipInt)
			cidrStr := common.JoinCIDRs(cidrPairs)

			confStr = strings.ReplaceAll(confStr, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = "+cidrStr)
			confStr = strings.ReplaceAll(confStr, "109.122.216.14:", "127.0.0.1:")

			err = os.WriteFile(confModPath, []byte(confStr), 0600)
			check(err)
		}

		c.String(200, "ok")
	})

	router.GET("/nodes", func(c *gin.Context) {
		req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/nodes", state.VpnHost), nil)
		if err != nil {
			if len(state.NodeCache) != 0 {
				log.Println(err)

				c.JSON(200, &state.NodeCache)
				return
			} else {
				panic(err)
			}
		}

		req.Header.Set("Authorization", state.Token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if len(state.NodeCache) != 0 {
				log.Println(err)

				c.JSON(200, &state.NodeCache)
				return
			} else {
				panic(err)
			}
		}

		defer resp.Body.Close()

		respBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			if len(state.NodeCache) != 0 {
				log.Println(err)

				c.JSON(200, &state.NodeCache)
				return
			} else {
				panic(err)
			}
		}

		var nodes []string
		err = json.Unmarshal(respBytes, &nodes)
		if err != nil {
			if len(state.NodeCache) != 0 {
				log.Println(err)

				c.JSON(200, &state.NodeCache)
				return
			} else {
				panic(err)
			}
		}

		slices.Sort(nodes)

		if len(state.NodeCache) != len(nodes) {
			state.NodeCache = nodes

			stateBytes, err := json.Marshal(&state)
			check(err)

			check(os.WriteFile(common.MoleguardState, stateBytes, 0600))
			c.JSON(200, &state)
		}

		c.JSON(200, &nodes)
	})

	router.POST("/state", func(c *gin.Context) {
		check(c.BindJSON(&state))

		stateBytes, err := json.Marshal(&state)
		check(err)

		check(os.WriteFile(common.MoleguardState, stateBytes, 0600))
		c.JSON(200, &state)
	})

	listener, err := net.Listen("unix", common.MoleguardSock)
	if err != nil {
		panic(err)
	}

	check(os.Chmod(common.MoleguardSock, 0666))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		cleanup()
		os.Exit(0)
	}()

	check(http.Serve(listener, router))
}
