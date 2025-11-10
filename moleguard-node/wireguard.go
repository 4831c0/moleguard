package main

import (
	"log"
	"os"
	"os/exec"
	"path"
	"sync"
)

var activeRelay string
var wgMutex sync.Mutex

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

func mullvadChange(relay string, confDir string) error {
	wgMutex.Lock()
	defer wgMutex.Unlock()

	log.Println("Tearing down old iptables rules")
	err := iptablesTeardown(activeRelay)

	log.Println("Disabling mullvad configs")
	err = downAll(confDir)
	if err != nil {
		return err
	}

	activeRelay = relay

	log.Println("Setting up new iptables rules")
	err = exec.Command(wgQuick, "up", path.Join(confDir, activeRelay+".conf")).Run()
	if err != nil {
		return err
	}

	log.Println("Setting up new iptables rules")
	return iptablesSetup(activeRelay)
}
