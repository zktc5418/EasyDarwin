package mp4

type MP4 struct {
	Ftyp *FtypBox
	Moov *MoovBox
}

func DefaultMp4Header() *MP4 {
	return &MP4{
		Ftyp: DefaultFtypBox(),
		Moov: DefaultMoovBox(),
	}
}
