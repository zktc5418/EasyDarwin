package mp4

import "io"

// Track Box (tkhd - mandatory)
//
// Contained in : Movie Box (moov)
//
// A media file can contain one or more tracks.
type TrakBox struct {
	Tkhd *TkhdBox
	Mdia *MdiaBox
}

func DecodeTrak(r io.Reader) (Box, error) {
	l, err := DecodeContainer(r)
	if err != nil {
		return nil, err
	}
	t := &TrakBox{}
	for _, b := range l {
		switch b.Type() {
		case "tkhd":
			t.Tkhd = b.(*TkhdBox)
		case "mdia":
			t.Mdia = b.(*MdiaBox)
		default:
			return nil, ErrBadFormat
		}
	}
	return t, nil
}

func (b *TrakBox) Type() string {
	return "trak"
}

func (b *TrakBox) Size() int {
	sz := b.Tkhd.Size()
	sz += b.Mdia.Size()
	return sz + BoxHeaderSize
}

func (b *TrakBox) Dump() {
	b.Tkhd.Dump()
	b.Mdia.Dump()
}

func (b *TrakBox) Encode(w io.Writer) error {
	err := EncodeHeader(b, w)
	if err != nil {
		return err
	}
	err = b.Tkhd.Encode(w)
	if err != nil {
		return err
	}
	return b.Mdia.Encode(w)
}
