package mp4

import (
	"fmt"
	"io"
)

// Movie Box (moov - mandatory)
//
// Status: partially decoded (anything other than mvhd, iods, trak or udta is ignored)
//
// Contains all meta-data. To be able to stream a file, the moov box should be placed before the mdat box.
type MoovBox struct {
	Mvhd *MvhdBox
	Trak []*TrakBox
}

func DefaultMoovBox() *MoovBox {
	return &MoovBox{
		Mvhd: DefaultMvhdBox(),
	}
}

func DecodeMoov(r io.Reader) (Box, error) {
	l, err := DecodeContainer(r)
	if err != nil {
		return nil, err
	}
	m := &MoovBox{}
	for _, b := range l {
		switch b.Type() {
		case "mvhd":
			m.Mvhd = b.(*MvhdBox)
		case "trak":
			m.Trak = append(m.Trak, b.(*TrakBox))
		}
	}
	return m, err
}

func (b *MoovBox) Type() string {
	return "moov"
}

func (b *MoovBox) Size() int {
	sz := b.Mvhd.Size()
	for _, t := range b.Trak {
		sz += t.Size()
	}
	return sz + BoxHeaderSize
}

func (b *MoovBox) Dump() {
	b.Mvhd.Dump()
	for i, t := range b.Trak {
		fmt.Println("Track", i)
		t.Dump()
	}
}

func (b *MoovBox) Encode(w io.Writer) error {
	err := EncodeHeader(b, w)
	if err != nil {
		return err
	}
	err = b.Mvhd.Encode(w)
	if err != nil {
		return err
	}
	for _, t := range b.Trak {
		err = t.Encode(w)
		if err != nil {
			return err
		}
	}
	return nil
}
