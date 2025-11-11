#!/bin/sh

sudo systemctl disable --now moleguard-daemon
sudo rm -rfv /usr/bin/moleguard-client /etc/moleguard-daemon /etc/moleguard /etc/systemd/system/moleguard-daemon.service
