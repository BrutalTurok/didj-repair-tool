package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// cabFiles extracts files from a CAB archive (MSZIP) embedded in an .exe, such
// as DidjPlugin.exe. Returns name → content for the files accepted by want().
func cabFiles(d []byte, want func(string) bool) (map[string][]byte, error) {
	o := bytes.Index(d, []byte("MSCF\x00\x00\x00\x00"))
	if o < 0 {
		return nil, fmt.Errorf("no es un archivo CAB")
	}
	le := binary.LittleEndian
	if o+36 > len(d) {
		return nil, fmt.Errorf("CAB truncado")
	}
	coffFiles := int(le.Uint32(d[o+16:]))
	cFolders := int(le.Uint16(d[o+26:]))
	cFiles := int(le.Uint16(d[o+28:]))
	flags := le.Uint16(d[o+30:])
	p := o + 36
	cbFolderRes, cbDataRes := 0, 0
	if flags&4 != 0 {
		cbHeaderRes := int(le.Uint16(d[p:]))
		cbFolderRes = int(d[p+2])
		cbDataRes = int(d[p+3])
		p += 4 + cbHeaderRes
	}
	type folder struct {
		off, blocks, typ int
	}
	var folders []folder
	for i := 0; i < cFolders; i++ {
		folders = append(folders, folder{int(le.Uint32(d[p:])), int(le.Uint16(d[p+4:])), int(le.Uint16(d[p+6:]))})
		p += 8 + cbFolderRes
	}
	type entry struct {
		name          string
		size, off, fi int
	}
	var entries []entry
	p = o + coffFiles
	for i := 0; i < cFiles; i++ {
		sz := int(le.Uint32(d[p:]))
		uoff := int(le.Uint32(d[p+4:]))
		fi := int(le.Uint16(d[p+8:]))
		p += 16
		e := bytes.IndexByte(d[p:], 0)
		name := string(d[p : p+e])
		p += e + 1
		entries = append(entries, entry{name, sz, uoff, fi})
	}
	cache := map[int][]byte{}
	decode := func(fi int) ([]byte, error) {
		if b, ok := cache[fi]; ok {
			return b, nil
		}
		f := folders[fi]
		if f.typ&0xF != 1 {
			return nil, fmt.Errorf("compresión CAB no soportada (%d)", f.typ&0xF)
		}
		var out []byte
		q := o + f.off
		for i := 0; i < f.blocks; i++ {
			cbData := int(le.Uint16(d[q+4:]))
			cbUnc := int(le.Uint16(d[q+6:]))
			q += 8 + cbDataRes
			blk := d[q : q+cbData]
			q += cbData
			if len(blk) < 2 || blk[0] != 'C' || blk[1] != 'K' {
				return nil, fmt.Errorf("bloque MSZIP no válido")
			}
			dict := out
			if len(dict) > 32768 {
				dict = dict[len(dict)-32768:]
			}
			r := flate.NewReaderDict(bytes.NewReader(blk[2:]), dict)
			buf := make([]byte, cbUnc)
			if _, err := io.ReadFull(r, buf); err != nil {
				return nil, fmt.Errorf("error descomprimiendo: %v", err)
			}
			out = append(out, buf...)
		}
		cache[fi] = out
		return out, nil
	}
	res := map[string][]byte{}
	for _, e := range entries {
		if !want(e.name) || e.fi >= len(folders) {
			continue
		}
		data, err := decode(e.fi)
		if err != nil {
			return nil, err
		}
		if e.off+e.size > len(data) {
			return nil, fmt.Errorf("CAB dañado")
		}
		res[e.name] = data[e.off : e.off+e.size]
	}
	return res, nil
}

func isLFP(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".lfp") }
