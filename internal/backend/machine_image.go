package backend

import (
	"encoding/binary"
	"fmt"
	"sort"
)

// PE/COFF writers use calculated layouts and RVAs, never a template executable.
func machineAlign(n, a int) int { return (n + a - 1) / a * a }
func pe64Image(code []byte, labels map[string]int, functions []x64Function) ([]byte, error) {
	entry, ok := labels["native_entry"]
	if !ok || len(code) == 0 {
		return nil, fmt.Errorf("missing machine entry")
	}
	type section struct {
		name     string
		data     []byte
		flags    uint32
		rva, raw int
	}
	// Emit unwind information for every generated fixed-frame function.
	var xdata, pdata []byte
	sorted := append([]x64Function(nil), functions...)
	sort.Slice(sorted, func(i, j int) bool { return labels[sorted[i].Label] < labels[sorted[j].Label] })
	textRVA := 0x1000
	xRVA := machineAlign(textRVA+len(code), 0x1000)
	for _, f := range sorted {
		if f.Frame <= 0 || f.Frame%16 != 0 || f.Frame >= 4096 {
			return nil, fmt.Errorf("unsupported Win64 frame %d", f.Frame)
		}
		off := len(xdata)
		// Version 1, 11-byte prologue: push rbp; mov rbp,rsp; sub rsp,imm32.
		// UWOP_ALLOC_LARGE (2 slots), UWOP_PUSH_NONVOL RBP (1 slot).
		xdata = append(xdata, 1, 11, 3, 0, 11, 1, byte(f.Frame/8), byte(f.Frame/8>>8), 1, 0x50, 0, 0)
		row := make([]byte, 12)
		binary.LittleEndian.PutUint32(row, uint32(textRVA+labels[f.Label]))
		binary.LittleEndian.PutUint32(row[4:], uint32(textRVA+labels[f.End]))
		binary.LittleEndian.PutUint32(row[8:], uint32(xRVA+off))
		pdata = append(pdata, row...)
	}
	secs := []section{{name: ".text", data: code, flags: 0x60000020, rva: textRVA}, {name: ".xdata", data: xdata, flags: 0x40000040, rva: xRVA}, {name: ".pdata", data: pdata, flags: 0x40000040, rva: machineAlign(xRVA+len(xdata), 0x1000)}}
	headers := machineAlign(0x80+4+20+240+40*len(secs), 512)
	size := headers
	for i := range secs {
		secs[i].raw = size
		size += machineAlign(len(secs[i].data), 512)
	}
	out := make([]byte, size)
	put16 := func(at int, v uint16) { binary.LittleEndian.PutUint16(out[at:], v) }
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(out[at:], v) }
	put64 := func(at int, v uint64) { binary.LittleEndian.PutUint64(out[at:], v) }
	copy(out, "MZ")
	put32(0x3c, 0x80)
	copy(out[0x80:], "PE\x00\x00")
	coff := 0x84
	put16(coff, 0x8664)
	put16(coff+2, uint16(len(secs)))
	put16(coff+16, 240)
	put16(coff+18, 0x23) // executable, large address aware, relocations stripped
	opt := coff + 20
	put16(opt, 0x20b)
	put32(opt+4, uint32(machineAlign(len(code), 512)))
	put32(opt+8, uint32(size-headers-machineAlign(len(code), 512)))
	put32(opt+16, uint32(textRVA+entry))
	put32(opt+20, uint32(textRVA))
	put64(opt+24, 0x140000000)
	put32(opt+32, 4096)
	put32(opt+36, 512)
	put16(opt+40, 6)
	put16(opt+48, 6)
	last := secs[len(secs)-1]
	put32(opt+56, uint32(machineAlign(last.rva+len(last.data), 4096)))
	put32(opt+60, uint32(headers))
	put16(opt+68, 3)
	put16(opt+70, 0x100)
	put64(opt+72, 1<<20)
	put64(opt+80, 4096)
	put64(opt+88, 1<<20)
	put64(opt+96, 4096)
	put32(opt+108, 16)
	put32(opt+112+3*8, uint32(secs[2].rva))
	put32(opt+112+3*8+4, uint32(len(pdata)))
	for i, s := range secs {
		at := opt + 240 + i*40
		copy(out[at:at+8], s.name)
		put32(at+8, uint32(len(s.data)))
		put32(at+12, uint32(s.rva))
		put32(at+16, uint32(machineAlign(len(s.data), 512)))
		put32(at+20, uint32(s.raw))
		put32(at+36, s.flags)
		copy(out[s.raw:], s.data)
	}
	return out, nil
}

func coff64Object(code []byte) []byte {
	// Internal PC-relative fixups are resolved inside .text. Export entry at 0.
	const headers = 60
	symbols := headers + len(code)
	out := make([]byte, symbols+18+4+len("native_entry")+1)
	binary.LittleEndian.PutUint16(out, 0x8664)
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint32(out[8:], uint32(symbols))
	binary.LittleEndian.PutUint32(out[12:], 1)
	copy(out[20:], ".text")
	binary.LittleEndian.PutUint32(out[36:], uint32(len(code)))
	binary.LittleEndian.PutUint32(out[40:], headers)
	binary.LittleEndian.PutUint32(out[56:], 0x60500020)
	copy(out[headers:], code)
	binary.LittleEndian.PutUint32(out[symbols+4:], 4)
	binary.LittleEndian.PutUint16(out[symbols+12:], 1)
	binary.LittleEndian.PutUint16(out[symbols+14:], 0x20)
	out[symbols+16] = 2
	binary.LittleEndian.PutUint32(out[symbols+18:], uint32(4+len("native_entry")+1))
	copy(out[symbols+22:], "native_entry")
	return out
}
