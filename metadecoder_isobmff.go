package imagemeta

import "strconv"

const (
	isobmffFlagTrackEnabled = 0x000001
)

// Decode the 'mvhd' atom.
func isobmffDecodeMVHD(s *streamReader, opts *Options) {
	// Read the metadata fields.
	versionAndFlags := s.read4()
	version := versionAndFlags >> 24
	var creationTime, modificationTime, duration uint64
	var timescale uint32
	switch version {
	case 0:
		creationTime = uint64(s.read4())
		modificationTime = uint64(s.read4())
		timescale = s.read4()
		duration = uint64(s.read4())
	case 1:
		creationTime = s.read8r(s.r)
		modificationTime = s.read8r(s.r)
		timescale = s.read4()
		duration = s.read8r(s.r)
	default:
		return // unsupported 'mvhd' atom
	}

	// Send the tag values to the caller.
	maybeHandle := func(name string, value any) {
		tagInfo := TagInfo{
			Source:    ISOBMFF,
			Tag:       name,
			Namespace: "MovieHeader",
			Value:     value,
		}
		if opts.ShouldHandleTag(tagInfo) {
			opts.HandleTag(tagInfo)
		}
	}
	maybeHandle("CreateDate", ISOBMFFTimestamp(creationTime))
	maybeHandle("ModifyDate", ISOBMFFTimestamp(modificationTime))
	maybeHandle("Duration", rat[uint64]{duration, uint64(timescale)})
}

func isobmffDecodeTKHD(s *streamReader, opts *Options, config *ImageConfig) {
	// Read all the fields from this atom.
	versionAndFlags := s.read4()
	version := versionAndFlags >> 24
	flags := versionAndFlags & 0xffffff
	if flags&isobmffFlagTrackEnabled == 0 {
		return
	}
	var creationTime, modificationTime uint64
	var trackID uint32
	switch version {
	case 0:
		creationTime = uint64(s.read4())
		modificationTime = uint64(s.read4())
		trackID = s.read4()
		s.skip(8) // skip: reserved (4) and duration (4)
	case 1:
		creationTime = s.read8r(s.r)
		modificationTime = s.read8r(s.r)
		trackID = s.read4()
		s.skip(12) // skip: reserved (4) and duration (8)
	default:
		return // unsupported 'tkhd' atom
	}
	s.skip(8) // reserved
	layer := int16(s.read2())
	s.skip(2) // skip: alternate group (not in exiftool)
	volume := int16(s.read2())
	s.skip(2) // reserved
	var matrix ISOBMFFMatrixStructure
	for i := range 9 {
		matrix[i] = int32(s.read4())
	}
	width := int32(s.read4())
	height := int32(s.read4())

	// Send the tag values to the caller.
	if opts.Sources.Has(CONFIG) && width != 0 && height != 0 {
		config.Width = int(width) >> 16
		config.Height = int(height) >> 16
	}
	if opts.Sources.Has(ISOBMFF) {
		namespace := "Track" + strconv.FormatUint(uint64(trackID), 10)
		maybeHandle := func(name string, value any) {
			tagInfo := TagInfo{
				Source:    ISOBMFF,
				Tag:       name,
				Namespace: namespace,
				Value:     value,
			}
			if opts.ShouldHandleTag(tagInfo) {
				opts.HandleTag(tagInfo)
			}
		}

		// Exiftool apparently has a different subset of tags for audio and
		// video streams.
		// It also seems to use different tag names for audio and video streams
		// (notably for create/modify date), not sure why that is but to make
		// sure we are able to test these tags against exiftool we'll match
		// their behavior.
		if width != 0 && height != 0 {
			// Video stream
			maybeHandle("MediaCreateDate", ISOBMFFTimestamp(creationTime))
			maybeHandle("MediaModifyDate", ISOBMFFTimestamp(modificationTime))
			maybeHandle("ImageWidth", rat[int32]{width, 1 << 16})
			maybeHandle("ImageHeight", rat[int32]{height, 1 << 16})
			maybeHandle("MatrixStructure", matrix)
		} else {
			// Audio stream
			maybeHandle("TrackCreateDate", ISOBMFFTimestamp(creationTime))
			maybeHandle("TrackModifyDate", ISOBMFFTimestamp(modificationTime))
			maybeHandle("TrackID", trackID)
			maybeHandle("TrackLayer", layer)
			maybeHandle("TrackVolume", rat[int32]{int32(volume), 256})
		}
	}
}
