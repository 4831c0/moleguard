package common

import "path"

const MoleguardDir = "/etc/moleguard"

var MoleguardState = path.Join(MoleguardDir, "state.json")
var MoleguardChisel = path.Join(MoleguardDir, "chisel.json")
var MoleguardSock = path.Join(MoleguardDir, "daemon.sock")
var MoleguardWgConfDir = path.Join(MoleguardDir, "conf-raw")
var MoleguardWgConfActive = path.Join(MoleguardDir, "conf-tmp")
