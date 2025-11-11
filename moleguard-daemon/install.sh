#!/bin/sh

go build -v
go build -v ../moleguard-client/
sudo cp ./moleguard-client /usr/bin/
sudo cp -v moleguard-daemon /etc/moleguard-daemon
sudo cp -v moleguard-daemon.service /etc/systemd/system/moleguard-daemon.service
sudo systemctl enable --now moleguard-daemon