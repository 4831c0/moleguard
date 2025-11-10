FROM ghcr.io/linuxserver/baseimage-alpine:3.22 AS builder

RUN apk add git go

# Mullvad wgephemeralpeer
RUN mkdir /root/wgephemeralpeer
WORKDIR /root/wgephemeralpeer
RUN git clone https://github.com/mullvad/wgephemeralpeer .
RUN go build -v cmd/mullvad-upgrade-tunnel/main.go

RUN mkdir /root/moleguard-node
WORKDIR /root/moleguard-node
COPY *.go /root/moleguard-node/
COPY *.mod /root/moleguard-node/
RUN go build -v

FROM lscr.io/linuxserver/wireguard:latest

RUN apk add python3 py3-cryptography

COPY ./etc/ /etc/
COPY wg-tools/wg-mullvad.py /root/wg-mullvad.py

COPY --from=builder /root/moleguard-node/moleguard-node /root/moleguard-node
COPY --from=builder /root/wgephemeralpeer/main /root/mullvad-upgrade-tunnel

RUN chmod +x /root/moleguard-node /root/mullvad-upgrade-tunnel
RUN sed -i 's/qrencode.*//g' /etc/s6-overlay/s6-rc.d/init-wireguard-confs/run
ENTRYPOINT [ "/init" ]