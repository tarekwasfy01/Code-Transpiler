//go:build cgo

package matrixir

/*
#cgo CFLAGS: -I${SRCDIR}/../../oracle/include
#include "scanner_bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type cgoExternalScanner struct {
	language string
	payload  unsafe.Pointer
}

func newCGOExternalScanner(language string) (*cgoExternalScanner, error) {
	cl := C.CString(language)
	defer C.free(unsafe.Pointer(cl))
	if C.uct_scanner_available(cl) == 0 {
		return nil, fmt.Errorf("external scanner unavailable for %s", language)
	}
	p := C.uct_scanner_create(cl)
	if p == nil {
		return nil, fmt.Errorf("external scanner create failed for %s", language)
	}
	return &cgoExternalScanner{language: language, payload: p}, nil
}

func (s *cgoExternalScanner) Close() {
	if s == nil || s.payload == nil {
		return
	}
	cl := C.CString(s.language)
	defer C.free(unsafe.Pointer(cl))
	C.uct_scanner_destroy(cl, s.payload)
	s.payload = nil
}

func (s *cgoExternalScanner) Scan(source string, offset int, valid []bool) (ExternalScanResult, error) {
	if s == nil || s.payload == nil {
		return ExternalScanResult{}, fmt.Errorf("external scanner is closed")
	}
	if offset < 0 || offset > len(source) {
		return ExternalScanResult{}, fmt.Errorf("invalid scanner offset %d", offset)
	}
	cs := C.CBytes([]byte(source))
	defer C.free(cs)
	flags := make([]C.uint8_t, len(valid))
	for i, v := range valid {
		if v {
			flags[i] = 1
		}
	}
	in := C.UCTScannerInput{source: (*C.char)(cs), length: C.size_t(len(source)), position: C.size_t(offset), token_start: C.size_t(offset), mark_end: C.size_t(offset), lookahead: 0}
	if offset < len(source) {
		in.lookahead = C.int32_t(source[offset])
	}
	cl := C.CString(s.language)
	defer C.free(unsafe.Pointer(cl))
	var vp *C.uint8_t
	if len(flags) > 0 {
		vp = (*C.uint8_t)(unsafe.Pointer(&flags[0]))
	}
	ok := C.uct_scanner_scan(cl, s.payload, &in, vp, C.size_t(len(flags)))
	if ok == 0 {
		return ExternalScanResult{}, nil
	}
	buf := make([]byte, 1024)
	n := C.uct_scanner_serialize(cl, s.payload, (*C.char)(unsafe.Pointer(&buf[0])))
	if n > C.size_t(len(buf)) {
		n = C.size_t(len(buf))
	}
	return ExternalScanResult{Accepted: true, AcceptedSymbol: int(in.result_symbol), EndOffset: int(in.mark_end), Serialized: append([]byte(nil), buf[:int(n)]...)}, nil
}

func (s *cgoExternalScanner) Restore(state []byte) {
	if s == nil || s.payload == nil {
		return
	}
	cl := C.CString(s.language)
	defer C.free(unsafe.Pointer(cl))
	var p *C.char
	if len(state) > 0 {
		p = (*C.char)(unsafe.Pointer(&state[0]))
	}
	C.uct_scanner_deserialize(cl, s.payload, p, C.size_t(len(state)))
}

func (s *cgoExternalScanner) Clone(state []byte) (externalScannerRuntime, error) {
	if s == nil || s.payload == nil {
		return nil, fmt.Errorf("external scanner is closed")
	}
	n, err := newCGOExternalScanner(s.language)
	if err != nil {
		return nil, err
	}
	n.Restore(state)
	return n, nil
}
