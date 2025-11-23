package common

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
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

func parseCIDRtoRange(cidr string) (start, end uint32, ok bool) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil || ipnet == nil {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return 0, 0, false
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return 0, 0, false
		}
		u := binary.BigEndian.Uint32(ip4)
		return u, u, true
	}
	ip4 := ipnet.IP.To4()
	if ip4 == nil {
		return 0, 0, false
	}
	startU := binary.BigEndian.Uint32(ip4)
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return 0, 0, false
	}
	if ones < 0 || ones > 32 {
		return 0, 0, false
	}
	size := uint32(1) << (32 - uint32(ones))
	endU := startU + size - 1
	return startU, endU, true
}

func BuildCIDRsExcept(excludes ...string) []cidrPair {
	type rng struct{ s, e uint32 }
	var exRanges []rng
	for _, c := range excludes {
		s, e, ok := parseCIDRtoRange(strings.TrimSpace(c))
		if !ok {
			continue
		}
		exRanges = append(exRanges, rng{s: s, e: e})
	}
	if len(exRanges) == 0 {
		return []cidrPair{{Start: 0, Prefix: 0}}
	}

	sort.Slice(exRanges, func(i, j int) bool {
		if exRanges[i].s == exRanges[j].s {
			return exRanges[i].e < exRanges[j].e
		}
		return exRanges[i].s < exRanges[j].s
	})
	merged := make([]rng, 0, len(exRanges))
	cur := exRanges[0]
	for i := 1; i < len(exRanges); i++ {
		if exRanges[i].s <= cur.e+1 {
			if exRanges[i].e > cur.e {
				cur.e = exRanges[i].e
			}
		} else {
			merged = append(merged, cur)
			cur = exRanges[i]
		}
	}
	merged = append(merged, cur)

	var res []cidrPair
	var start uint32 = 0
	idx := 0
	for start != 0xFFFFFFFF {
		if idx < len(merged) && start > merged[idx].e {
			idx++
			continue
		}

		if idx < len(merged) && merged[idx].s <= start && start <= merged[idx].e {
			if merged[idx].e == 0xFFFFFFFF {
				return res
			}
			start = merged[idx].e + 1
			if start == 0 {
				return res
			}
			idx++
			continue
		}

		var maxAllow uint32 = 0xFFFFFFFF
		if idx < len(merged) {
			if merged[idx].s == 0 {
				if merged[idx].e == 0xFFFFFFFF {
					return res
				}
				start = merged[idx].e + 1
				if start == 0 {
					return res
				}
				idx++
				continue
			}
			maxAllow = merged[idx].s - 1
		}

		maxPref := maxAlignedPrefix(start)

		prefix := maxPref
		for prefix <= 32 {
			blockSize := uint32(1) << (32 - prefix)
			blockEnd := start + blockSize - 1
			if blockEnd > maxAllow {
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
