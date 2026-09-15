// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

// PE/COFF writers use calculated layouts and RVAs, never a template executable.
func machineAlign(n, a int) int { return (n + a - 1) / a * a }

func splitPEData(code []byte, labels map[string]int) ([]byte, []byte, []byte, map[string]int, error) {
	readonly, mutable := -1, -1
	for name, off := range labels {
		if off < 0 || off >= len(code) {
			continue
		}
		if strings.HasPrefix(name, "uast_") && !strings.HasPrefix(name, "uast_global_") && (readonly < 0 || off < readonly) {
			readonly = off
		}
		if (strings.HasPrefix(name, "uast_global_") || strings.HasPrefix(name, "__project_data_")) && (mutable < 0 || off < mutable) {
			mutable = off
		}
	}
	dataStart := len(code)
	if readonly >= 0 && readonly < dataStart {
		dataStart = readonly
	}
	if mutable >= 0 && mutable < dataStart {
		dataStart = mutable
	}
	if dataStart == len(code) {
		return code, nil, nil, labels, nil
	}
	if mutable >= 0 && readonly >= 0 && mutable < readonly {
		return nil, nil, nil, nil, fmt.Errorf("mutable data precedes readonly data")
	}
	text := append([]byte(nil), code[:dataStart]...)
	rdataEnd := len(code)
	if mutable >= 0 {
		rdataEnd = mutable
	}
	readonlyStart := readonly
	if readonlyStart < 0 {
		readonlyStart = dataStart
	}
	rdata := append([]byte(nil), code[readonlyStart:rdataEnd]...)
	data := []byte(nil)
	if mutable >= 0 {
		data = append([]byte(nil), code[mutable:]...)
	}
	adjusted := map[string]int{}
	for name, off := range labels {
		switch {
		case off < dataStart:
			adjusted[name] = off
		case mutable >= 0 && off >= mutable:
			adjusted[name] = off - mutable
		case readonly >= 0 && off >= readonly:
			adjusted[name] = off - readonly
		}
	}
	return text, rdata, data, adjusted, nil
}

func patchPERIPData(text []byte, oldLabels, newLabels map[string]int, textRVA, rdataRVA, dataRVA int) error {
	byOld := map[int]string{}
	for name, off := range oldLabels {
		byOld[off] = name
	}
	for i := 0; i+7 <= len(text); i++ {
		if (text[i]&0xf0) != 0x40 || text[i+1] != 0x8d || text[i+2]&0xc7 != 0x05 {
			continue
		}
		disp := int32(binary.LittleEndian.Uint32(text[i+3 : i+7]))
		oldTarget := i + 7 + int(disp)
		name, ok := byOld[oldTarget]
		if !ok || (!strings.HasPrefix(name, "uast_") && !strings.HasPrefix(name, "__project_data_")) {
			continue
		}
		off, ok := newLabels[name]
		if !ok {
			return fmt.Errorf("missing relocated data label %q", name)
		}
		targetRVA := rdataRVA + off
		if strings.HasPrefix(name, "__project_data_") || strings.HasPrefix(name, "uast_global_") {
			targetRVA = dataRVA + off
		}
		newDisp := int64(targetRVA) - int64(textRVA+i+7)
		if newDisp < -2147483648 || newDisp > 2147483647 {
			return fmt.Errorf("data RIP relocation out of range for %q", name)
		}
		binary.LittleEndian.PutUint32(text[i+3:i+7], uint32(int32(newDisp)))
	}
	return nil
}

// pe64ImportSpec describes one PE import by its DLL and exported procedure.
// The linker keeps this representation target-neutral; callers provide only
// the ABI symbol identity, while the image writer lays out the standard PE
// descriptor, ILT, IAT and hint/name records.
type pe64ImportSpec struct {
	DLL  string
	Name string
}

// pe64ImportSection builds a deterministic PE32+ import section.  The result
// is self-contained and uses RVAs relative to sectionRVA, so it can be placed
// after text/data without reparsing or copying semantic state.  The returned
// iatRVA map identifies each imported symbol's IAT slot for thunk generation.
func pe64ImportSection(sectionRVA int, specs []pe64ImportSpec) (data []byte, iatRVA map[string]int, err error) {
	if sectionRVA <= 0 {
		return nil, nil, fmt.Errorf("invalid import section RVA %d", sectionRVA)
	}
	uniq := map[string]pe64ImportSpec{}
	for _, s := range specs {
		s.DLL = strings.TrimSpace(s.DLL)
		s.Name = strings.TrimSpace(s.Name)
		if s.DLL == "" || s.Name == "" || strings.IndexByte(s.DLL, 0) >= 0 || strings.IndexByte(s.Name, 0) >= 0 {
			return nil, nil, fmt.Errorf("invalid PE import %q!%q", s.DLL, s.Name)
		}
		key := strings.ToLower(s.DLL) + "!" + s.Name
		uniq[key] = s
	}
	ordered := make([]pe64ImportSpec, 0, len(uniq))
	for _, s := range uniq {
		ordered = append(ordered, s)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if strings.EqualFold(ordered[i].DLL, ordered[j].DLL) {
			return ordered[i].Name < ordered[j].Name
		}
		return strings.ToLower(ordered[i].DLL) < strings.ToLower(ordered[j].DLL)
	})
	if len(ordered) == 0 {
		return nil, map[string]int{}, nil
	}
	iatRVA = make(map[string]int, len(ordered))
	type group struct {
		dll     string
		entries []pe64ImportSpec
	}
	groups := make([]group, 0)
	for _, s := range ordered {
		if len(groups) == 0 || !strings.EqualFold(groups[len(groups)-1].dll, s.DLL) {
			groups = append(groups, group{dll: s.DLL})
		}
		groups[len(groups)-1].entries = append(groups[len(groups)-1].entries, s)
	}
	// descriptors + one null descriptor; arrays and strings are appended after.
	descSize := (len(groups) + 1) * 20
	buf := make([]byte, descSize)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(buf[off:off+4], v) }
	appendAligned := func(b []byte, align int) int {
		for len(buf)%align != 0 {
			buf = append(buf, 0)
		}
		at := len(buf)
		buf = append(buf, b...)
		return at
	}
	for gi, g := range groups {
		ilt := make([]byte, (len(g.entries)+1)*8)
		iat := make([]byte, (len(g.entries)+1)*8)
		for ei, s := range g.entries {
			name := make([]byte, 2+len(s.Name)+1)
			copy(name[2:], s.Name)
			ho := appendAligned(name, 2)
			binary.LittleEndian.PutUint64(ilt[ei*8:], uint64(sectionRVA+ho))
			binary.LittleEndian.PutUint64(iat[ei*8:], uint64(sectionRVA+ho))
		}
		io := appendAligned(ilt, 8)
		ao := appendAligned(iat, 8)
		do := appendAligned(append([]byte(g.dll), 0), 1)
		base := gi * 20
		put32(base, uint32(sectionRVA+io))
		put32(base+12, uint32(sectionRVA+do))
		put32(base+16, uint32(sectionRVA+ao))
		for _, s := range g.entries {
			key := strings.ToLower(s.DLL) + "!" + s.Name
			iatRVA[key] = sectionRVA + ao
		}
		// advance per-entry IAT slot addresses deterministically
		for ei, s := range g.entries {
			key := strings.ToLower(s.DLL) + "!" + s.Name
			iatRVA[key] = sectionRVA + ao + ei*8
		}
	}
	return buf, iatRVA, nil
}

func pe64Image(code []byte, labels map[string]int, functions []x64Function, importSets ...[]pe64ImportSpec) ([]byte, error) {
	oldLabels := labels
	text, rdata, data, relocatedLabels, err := splitPEData(code, labels)
	if err != nil {
		return nil, err
	}
	labels = relocatedLabels
	entry, ok := labels["native_entry"]
	if !ok || len(text) == 0 {
		return nil, fmt.Errorf("missing machine entry")
	}
	code = text
	textRVA := 0x1000
	type section struct {
		name     string
		data     []byte
		flags    uint32
		rva, raw int
	}
	// Emit unwind information for every generated fixed-frame function. The
	// selector probes large frames page-by-page; UWOP_ALLOC_LARGE represents the
	// complete aligned frame size in the PE unwind record.
	var xdata, pdata []byte
	sorted := append([]x64Function(nil), functions...)
	sort.Slice(sorted, func(i, j int) bool { return labels[sorted[i].Label] < labels[sorted[j].Label] })
	rdataRVA := machineAlign(textRVA+len(code), 0x1000)
	dataRVA := machineAlign(rdataRVA+len(rdata), 0x1000)
	dataEndRVA := dataRVA
	if len(data) > 0 {
		dataEndRVA += len(data)
	} else {
		dataEndRVA = rdataRVA + len(rdata)
	}
	xRVA := machineAlign(dataEndRVA, 0x1000)
	for _, f := range sorted {
		if f.Frame <= 0 || f.Frame%16 != 0 || uint64(f.Frame) > math.MaxUint32 {
			return nil, fmt.Errorf("unsupported Win64 frame %d", f.Frame)
		}
		off := len(xdata)
		// Version 1, 11-byte prologue: push rbp; mov rbp,rsp; sub rsp,imm32.
		// UWOP_ALLOC_LARGE has two encodings.  The compact form stores
		// frame/8 in one 16-bit slot; large frames use opinfo=1 and store the
		// byte size in two 16-bit slots.  Both are followed by the RBP push
		// code.  Keeping this in the generic image writer avoids an arbitrary
		// per-function frame ceiling during module builds.
		if f.Frame/8 <= 0xffff {
			// header + 3 unwind slots (two for allocation, one for push) + pad
			xdata = append(xdata, 1, 11, 3, 0, 11, 1, byte(f.Frame/8), byte(f.Frame/8>>8), 1, 0x50, 0, 0)
		} else {
			// header + 4 unwind slots (three for allocation, one for push)
			xdata = append(xdata, 1, 11, 4, 0, 11, 0x11,
				byte(f.Frame), byte(f.Frame>>8), byte(f.Frame>>16), byte(f.Frame>>24),
				1, 0x50)
		}
		row := make([]byte, 12)
		binary.LittleEndian.PutUint32(row, uint32(textRVA+labels[f.Label]))
		binary.LittleEndian.PutUint32(row[4:], uint32(textRVA+labels[f.End]))
		binary.LittleEndian.PutUint32(row[8:], uint32(xRVA+off))
		pdata = append(pdata, row...)
	}
	imports := []pe64ImportSpec(nil)
	if len(importSets) > 0 {
		imports = importSets[0]
	}
	// Keep the import section after both unwind sections.  Using the end of
	// xdata here aliases .idata with .pdata (both are page aligned), producing
	// an invalid PE section layout once imports are present.
	pdataRVA := machineAlign(xRVA+len(xdata), 0x1000)
	idataRVA := machineAlign(pdataRVA+len(pdata), 0x1000)
	idata, iat, err := pe64ImportSection(idataRVA, imports)
	if err != nil {
		return nil, err
	}
	if err := patchPERIPData(code, oldLabels, labels, textRVA, rdataRVA, dataRVA); err != nil {
		return nil, err
	}
	secs := []section{{name: ".text", data: code, flags: 0xE0000060, rva: textRVA}}
	if len(rdata) > 0 {
		secs = append(secs, section{name: ".rdata", data: rdata, flags: 0x40000040, rva: rdataRVA})
	}
	if len(data) > 0 {
		secs = append(secs, section{name: ".data", data: data, flags: 0xC0000040, rva: dataRVA})
	}
	secs = append(secs, section{name: ".xdata", data: xdata, flags: 0x40000040, rva: xRVA}, section{name: ".pdata", data: pdata, flags: 0x40000040, rva: pdataRVA})
	if len(idata) > 0 {
		secs = append(secs, section{name: ".idata", data: idata, flags: 0xC0000040, rva: idataRVA})
	}
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
	pdataSectionRVA := 0
	for _, s := range secs {
		if s.name == ".pdata" {
			pdataSectionRVA = s.rva
			break
		}
	}
	put32(opt+112+3*8, uint32(pdataSectionRVA))
	put32(opt+112+3*8+4, uint32(len(pdata)))
	if len(idata) > 0 {
		put32(opt+112+1*8, uint32(idataRVA))
		put32(opt+112+1*8+4, uint32(len(idata)))
		// code is the instruction-only .text returned by splitPEData. Literal
		// bytes live in .rdata/.data and can never be mistaken for an FF /2 call.
		for i := 0; i+6 <= len(code); i++ {
			if code[i] == 0xff && code[i+1] == 0x15 {
				sentinel := binary.LittleEndian.Uint32(code[i+2 : i+6])
				keys := map[uint32]string{
					0x11111111: "kernel32.dll!LoadLibraryA",
					0x22222222: "kernel32.dll!GetProcAddress",
					0x33333333: "msvcrt.dll!printf",
					0x44444444: "kernel32.dll!GetStdHandle",
					0x55555555: "kernel32.dll!WriteFile",
					0x66666666: "kernel32.dll!Sleep",
					0x77777777: "kernel32.dll!FreeLibrary",
					0x88888888: "kernel32.dll!ExitProcess",
				}
				key, known := keys[sentinel]
				if !known {
					return nil, fmt.Errorf("PE import relocation at text offset %#x has unknown call_iat identity %#08x", i, sentinel)
				}
				slot, ok := iat[key]
				if !ok {
					return nil, fmt.Errorf("PE import relocation at text offset %#x references %s but the import table has no matching slot", i, key)
				}
				disp := int64(slot) - int64(textRVA+i+6)
				if disp < math.MinInt32 || disp > math.MaxInt32 {
					return nil, fmt.Errorf("PE import relocation at text offset %#x is out of RIP-relative range", i)
				}
				binary.LittleEndian.PutUint32(code[i+2:i+6], uint32(int32(disp)))
			}
		}
	}
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
	return coff64ObjectNamedAt(code, "native_entry", 0)
}

func coff64ObjectNamed(code []byte, symbol string) []byte {
	return coff64ObjectNamedAt(code, symbol, 0)
}

func coff64ObjectNamedAt(code []byte, symbol string, start int) []byte {
	// Internal PC-relative fixups are resolved inside .text. Export entry at 0.
	const headers = 60
	symbols := headers + len(code)
	if symbol == "" {
		symbol = "native_entry"
	}
	out := make([]byte, symbols+18+4+len(symbol)+1)
	binary.LittleEndian.PutUint16(out, 0x8664)
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint32(out[8:], uint32(symbols))
	binary.LittleEndian.PutUint32(out[12:], 1)
	copy(out[20:], ".text")
	binary.LittleEndian.PutUint32(out[36:], uint32(len(code)))
	binary.LittleEndian.PutUint32(out[40:], headers)
	binary.LittleEndian.PutUint32(out[56:], 0x60500020)
	copy(out[headers:], code)
	binary.LittleEndian.PutUint32(out[symbols:], uint32(start))
	binary.LittleEndian.PutUint32(out[symbols+4:], 4)
	binary.LittleEndian.PutUint16(out[symbols+12:], 1)
	binary.LittleEndian.PutUint16(out[symbols+14:], 0x20)
	out[symbols+16] = 2
	binary.LittleEndian.PutUint32(out[symbols+18:], uint32(4+len("native_entry")+1))
	copy(out[symbols+22:], symbol)
	return out
}

// linkCOFFObjects composes the function staging objects back into one .text
// stream. Symbol values are original program offsets, so PC-relative fixups
// remain valid when sections are placed at their recorded locations. The
// result is intentionally independent of the in-memory x64Program.
func linkCOFFObjects(objects map[string][]byte) ([]byte, error) {
	if len(objects) == 0 {
		return nil, fmt.Errorf("no COFF objects to link")
	}
	type section struct {
		start int
		data  []byte
	}
	sections := make([]section, 0, len(objects))
	maxEnd := 0
	for name, object := range objects {
		if len(object) < 60 || binary.LittleEndian.Uint16(object) != 0x8664 {
			return nil, fmt.Errorf("object %q has invalid COFF header", name)
		}
		rawSize := int(binary.LittleEndian.Uint32(object[36:]))
		rawAt := int(binary.LittleEndian.Uint32(object[40:]))
		if rawSize < 0 || rawAt < 0 || rawAt+rawSize > len(object) {
			return nil, fmt.Errorf("object %q has invalid text section", name)
		}
		symbolAt := int(binary.LittleEndian.Uint32(object[8:]))
		if symbolAt < 60 || symbolAt+18 > len(object) {
			return nil, fmt.Errorf("object %q has invalid symbol table", name)
		}
		start := int(binary.LittleEndian.Uint32(object[symbolAt:]))
		if start < 0 {
			return nil, fmt.Errorf("object %q has invalid symbol offset", name)
		}
		data := append([]byte(nil), object[rawAt:rawAt+rawSize]...)
		sections = append(sections, section{start: start, data: data})
		if start+len(data) > maxEnd {
			maxEnd = start + len(data)
		}
	}
	if maxEnd == 0 {
		return nil, fmt.Errorf("COFF objects contain no text")
	}
	linked := make([]byte, maxEnd)
	covered := make([]bool, maxEnd)
	for _, s := range sections {
		for i, b := range s.data {
			at := s.start + i
			if at >= len(linked) {
				return nil, fmt.Errorf("COFF section exceeds linked text")
			}
			if covered[at] && linked[at] != b {
				return nil, fmt.Errorf("COFF section overlap at %d", at)
			}
			linked[at], covered[at] = b, true
		}
	}
	for i, ok := range covered {
		if !ok {
			return nil, fmt.Errorf("COFF objects leave text gap at %d", i)
		}
	}
	return linked, nil
}
