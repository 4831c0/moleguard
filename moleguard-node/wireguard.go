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
	if err := run(iptables, "-A", "FORWARD", "-o", "eth0@if20", "!", "-d", "10.13.13.1/24", "-j", "REJECT"); err != nil {
		return err
	}
	if err := run(iptables, "-A", "FORWARD", "-i", newRelay, "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := run(iptables, "-A", "FORWARD", "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := run(iptables, "-A", "FORWARD", "-j", "REJECT"); err != nil {
		return err
	}

	// NAT

	if err := run("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", "eth0@if20", "-j", "MASQUERADE"); err != nil {
		return err
	}
	if err := run("iptables", "-t", "nat", "-A", "POSTROUTING", "-o", newRelay, "-j", "MASQUERADE"); err != nil {
		return err
	}

	return nil
}

func iptablesTeardown(oldRelay string) error {
	// Forwarding
	if err := run(iptables, "-D", "FORWARD", "-o", "eth0@if20", "!", "-d", "10.13.13.1/24", "-j", "REJECT"); err != nil {
		return err
	}
	if err := run(iptables, "-D", "FORWARD", "-i", oldRelay, "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := run(iptables, "-D", "FORWARD", "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := run(iptables, "-D", "FORWARD", "-j", "REJECT"); err != nil {
		return err
	}

	// NAT

	if err := run("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", "eth0@if20", "-j", "MASQUERADE"); err != nil {
		return err
	}
	if err := run("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", oldRelay, "-j", "MASQUERADE"); err != nil {
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
	err = iptablesSetup(activeRelay)
	if err != nil {
		return err
	}

	log.Println("Enabling mullvad")
	err = run(wgQuick, "up", path.Join(confDir, activeRelay+".conf"))
	if err != nil {
		return err
	}

	log.Println("Upgrading tunnel to post quantum-tunnel")
	return run(mullvadUpgradeTunnel, "-wg-interface", activeRelay)
}
