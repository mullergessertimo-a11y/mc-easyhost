package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
)

// A minimal reader for Minecraft's NBT format, enough to read level.dat.
// Compounds become map[string]any, lists []any, numbers their Go types.

const maxNBTDepth = 64

type nbtReader struct {
	r     *bufio.Reader
	depth int
}

func readLevelDat(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	nr := &nbtReader{r: bufio.NewReader(io.LimitReader(gz, 64<<20))}
	tag, err := nr.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if tag != 10 {
		return nil, errors.New("level.dat does not start with a compound")
	}
	if _, err := nr.str(); err != nil {
		return nil, err
	}
	v, err := nr.payload(10)
	if err != nil {
		return nil, err
	}
	return v.(map[string]any), nil
}

func (n *nbtReader) str() (string, error) {
	var l uint16
	if err := binary.Read(n.r, binary.BigEndian, &l); err != nil {
		return "", err
	}
	b := make([]byte, l)
	_, err := io.ReadFull(n.r, b)
	return string(b), err // Java's modified UTF-8; fine for names
}

func (n *nbtReader) length() (int, error) {
	var l int32
	if err := binary.Read(n.r, binary.BigEndian, &l); err != nil {
		return 0, err
	}
	if l < 0 || l > 16<<20 {
		return 0, errors.New("bad NBT length")
	}
	return int(l), nil
}

func (n *nbtReader) payload(tag byte) (any, error) {
	be := binary.BigEndian
	switch tag {
	case 1:
		var v int8
		return v, binary.Read(n.r, be, &v)
	case 2:
		var v int16
		return v, binary.Read(n.r, be, &v)
	case 3:
		var v int32
		err := binary.Read(n.r, be, &v)
		return v, err
	case 4:
		var v int64
		err := binary.Read(n.r, be, &v)
		return v, err
	case 5:
		var v uint32
		err := binary.Read(n.r, be, &v)
		return math.Float32frombits(v), err
	case 6:
		var v uint64
		err := binary.Read(n.r, be, &v)
		return math.Float64frombits(v), err
	case 7, 11, 12:
		l, err := n.length()
		if err != nil {
			return nil, err
		}
		size := map[byte]int{7: 1, 11: 4, 12: 8}[tag]
		_, err = io.CopyN(io.Discard, n.r, int64(l*size)) // arrays aren't needed
		return nil, err
	case 8:
		return n.str()
	case 9:
		elem, err := n.r.ReadByte()
		if err != nil {
			return nil, err
		}
		l, err := n.length()
		if err != nil {
			return nil, err
		}
		if err := n.enter(); err != nil {
			return nil, err
		}
		defer n.leave()
		list := make([]any, 0, min(l, 1024))
		for i := 0; i < l; i++ {
			v, err := n.payload(elem)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case 10:
		if err := n.enter(); err != nil {
			return nil, err
		}
		defer n.leave()
		m := map[string]any{}
		for {
			t, err := n.r.ReadByte()
			if err != nil {
				return nil, err
			}
			if t == 0 {
				return m, nil
			}
			name, err := n.str()
			if err != nil {
				return nil, err
			}
			v, err := n.payload(t)
			if err != nil {
				return nil, err
			}
			m[name] = v
		}
	case 0:
		return nil, nil
	}
	return nil, errors.New("unknown NBT tag")
}

func (n *nbtReader) enter() error {
	n.depth++
	if n.depth > maxNBTDepth {
		return errors.New("NBT nested too deeply")
	}
	return nil
}

func (n *nbtReader) leave() { n.depth-- }

// nbtPath walks nested compounds, e.g. nbtPath(root, "Data", "Version", "Name").
func nbtPath(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
