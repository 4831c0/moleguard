package common

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)

type cidrPair struct {
	Start  uint32
	Prefix int
}

func Uint32ToIPv4(u uint32) net.IP {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, u)
	return net.IP(b)
}

func cidrString(start uint32, prefix int) string {
	return fmt.Sprintf("%s/%d", Uint32ToIPv4(start).String(), prefix)
}

func JoinCIDRs(pairs []cidrPair) string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, cidrString(p.Start, p.Prefix))
	}
	return strings.Join(out, ", ")
}

func bitsTrailingZeros32(x uint32) int {
	if x == 0 {
		return 32
	}
	var n int
	for (x & 1) == 0 {
		n++
		x >>= 1
	}
	return n
}

func maxAlignedPrefix(start uint32) int {
	tz := bitsTrailingZeros32(start)
	return 32 - tz
}

func BuildCIDRsExcept(excluded uint32) []cidrPair {
	var res []cidrPair
	var start uint32 = 0

	for start != 0xFFFFFFFF {
		if start == excluded {
			start++
			if start == 0 {
				break
			}
			continue
		}

		maxPref := maxAlignedPrefix(start)

		prefix := maxPref
		for prefix <= 32 {
			blockSize := uint32(1) << (32 - prefix)
			blockEnd := start + blockSize - 1

			if excluded >= start && excluded <= blockEnd {
				prefix++
				continue
			}

			res = append(res, cidrPair{Start: start, Prefix: prefix})
			start += blockSize
			if start == 0 {
				return res
			}
			break
		}

		if prefix > 32 {
			res = append(res, cidrPair{Start: start, Prefix: 32})
			start++
			if start == 0 {
				break
			}
		}
	}

	return res
}

func IPv4ToUint32(s string) (uint32, error) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP")
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0, fmt.Errorf("not an IPv4 address")
	}
	return binary.BigEndian.Uint32(ip4), nil
}
