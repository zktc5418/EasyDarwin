package mp4

import "io"

// Sample Table Box (stbl - mandatory)
//
// Contained in : Media Information Box (minf)
//
// Status: partially decoded (anything other than stsd, stts, stsc, stss, stsz, stco, ctts is ignored)
//
// The table contains all information relevant to data samples (times, chunks, sizes, ...)
type StblBox struct {
	Stsd *StsdBox
	Stts *SttsBox
	Stsc *StscBox
	Stsz *StszBox
	Stco *StcoBox
}

func DecodeStbl(r io.Reader) (Box, error) {
	l, err := DecodeContainer(r)
	if err != nil {
		return nil, err
	}
	s := &StblBox{}
	for _, b := range l {
		switch b.Type() {
		case "stsd":
			s.Stsd = b.(*StsdBox)
		case "stts":
			s.Stts = b.(*SttsBox)
		case "stsc":
			s.Stsc = b.(*StscBox)
		case "stsz":
			s.Stsz = b.(*StszBox)
		case "stco":
			s.Stco = b.(*StcoBox)
		}
	}
	return s, nil
}

func (b *StblBox) Type() string {
	return "stbl"
}

func (b *StblBox) Size() int {
	sz := b.Stsd.Size()
	if b.Stts != nil {
		sz += b.Stts.Size()
	}
	if b.Stsc != nil {
		sz += b.Stsc.Size()
	}
	if b.Stsz != nil {
		sz += b.Stsz.Size()
	}
	if b.Stco != nil {
		sz += b.Stco.Size()
	}
	return sz + BoxHeaderSize
}

func (b *StblBox) Dump() {
	if b.Stsc != nil {
		b.Stsc.Dump()
	}
	if b.Stts != nil {
		b.Stts.Dump()
	}
	if b.Stsz != nil {
		b.Stsz.Dump()
	}
	if b.Stco != nil {
		b.Stco.Dump()
	}
}

func (b *StblBox) Encode(w io.Writer) error {
	err := EncodeHeader(b, w)
	if err != nil {
		return err
	}
	err = b.Stsd.Encode(w)
	if err != nil {
		return err
	}
	err = b.Stts.Encode(w)
	if err != nil {
		return err
	}
	err = b.Stsc.Encode(w)
	if err != nil {
		return err
	}
	err = b.Stsz.Encode(w)
	if err != nil {
		return err
	}
	err = b.Stco.Encode(w)
	if err != nil {
		return err
	}
	return nil
}
