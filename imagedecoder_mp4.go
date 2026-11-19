package imagemeta

import (
	"io"
)

// ISO base media file format specification:
// https://ossrs.io/lts/zh-cn/assets/files/ISO_IEC_14496-12-base-format-2012-b70dd5f101daecd072700609842c9649.pdf

// ISOBMFF box and item types used in MP4 containers.
var mp4FCC = struct {
	ftyp    fourCC
	moov    fourCC
	mvhd    fourCC
	trak    fourCC
	tkhd    fourCC
	uuid    fourCC
	xmpUUID [16]byte
}{
	ftyp:    fourCC{'f', 't', 'y', 'p'},
	moov:    fourCC{'m', 'o', 'o', 'v'},
	mvhd:    fourCC{'m', 'v', 'h', 'd'},
	trak:    fourCC{'t', 'r', 'a', 'k'},
	tkhd:    fourCC{'t', 'k', 'h', 'd'},
	uuid:    fourCC{'u', 'u', 'i', 'd'},
	xmpUUID: [16]byte{0xBE, 0x7A, 0xCF, 0xCB, 0x97, 0xA9, 0x42, 0xE8, 0x9C, 0x71, 0x99, 0x94, 0x91, 0xE3, 0xAF, 0xAC},
}

type imageDecoderMP4 struct {
	*baseStreamingDecoder
}

func (e *imageDecoderMP4) decode() error {
	sourceSet := (CONFIG | XMP | ISOBMFF) & e.opts.Sources

	// readBox reads an ISOBMFF box header from the current stream position.
	// Returns (startPos, totalBoxSize, boxType).
	// startPos: absolute stream position before the header.
	// totalBoxSize: total box size including header bytes (0 = extends to EOF).
	// After this call, the stream is positioned at the start of the box payload.
	readBox := func() (startPos int64, totalSize uint64, boxType fourCC) {
		startPos = e.pos()
		size := e.read4()
		e.readBytes(boxType[:])
		totalSize = uint64(size)
		if size == 1 {
			// Extended size: next 8 bytes hold the actual size.
			totalSize = e.read8r(e.r)
		}
		return
	}

	// Read the file header (this is a box like any other).
	ftypStart, ftypSize, ftypType := readBox()
	if e.isEOF {
		return errInvalidFormat
	}
	if ftypType != mp4FCC.ftyp {
		return errInvalidFormat
	}
	if ftypSize > 0 {
		e.seek(ftypStart + int64(ftypSize))
	}

	// Iterate through top-level boxes.
	for !sourceSet.IsZero() {
		boxStart, boxSize, boxType := readBox()
		if e.isEOF {
			return errInvalidFormat // unexpected EOF
		}

		// The 'moov' atom contains ISOBMFF metadata. It also includes video
		// width/height information.
		if boxType == mp4FCC.moov && sourceSet.Has(CONFIG|ISOBMFF) {
			moovEnd := boxStart + int64(boxSize)
			sourceSet = sourceSet.Remove(CONFIG | ISOBMFF)

			// Iterate through the boxes in the 'moov' box.
			for {
				boxStart, boxSize, boxType := readBox()
				if e.isEOF {
					return errInvalidFormat // unexpected EOF
				}

				// The 'mvhd' atom contains metadata about the movie as a whole
				// (timestamps, duration, etc).
				if boxType == mp4FCC.mvhd && e.opts.Sources.Has(ISOBMFF) {
					isobmffDecodeMVHD(e.streamReader, &e.opts)
				}

				// The 'trak' atom contains information about a single track
				// (usually: one for audio and one for video).
				if boxType == mp4FCC.trak {
					// Look for the 'tkhd' atom, which is the only one we're
					// interested in.
					for {
						boxStart, boxSize, boxType := readBox()
						if e.isEOF {
							return errInvalidFormat // unexpected EOF
						}
						if boxType == mp4FCC.tkhd {
							isobmffDecodeTKHD(e.streamReader, &e.opts, &e.result.ImageConfig)
							break // we don't need any other 'trak' boxes
						}
						if ftypSize > 0 {
							e.seek(boxStart + int64(boxSize))
						}
						if boxStart+int64(boxSize) == int64(moovEnd) {
							break
						}
					}
				}

				// Move to the next box inside this 'moov' box.
				if ftypSize > 0 {
					e.seek(boxStart + int64(boxSize))
				}
				if boxEnd := boxStart + int64(boxSize); boxEnd > moovEnd {
					return errInvalidFormat
				} else if boxEnd == int64(moovEnd) {
					break
				}
			}

			// We've gone through all the boxes.
			// No need to seek to the next one.
			continue
		}

		// XMP data is encoded in a 'uuid' box.
		// This type of box ('extended type') puts an UUID right between the box
		// header and the box content.
		if boxType == mp4FCC.uuid && sourceSet.Has(XMP) {
			var uuid [16]byte
			if err := e.readBytes(uuid[:]); err != nil {
				return errInvalidFormat
			}
			if uuid == mp4FCC.xmpUUID {
				sourceSet = sourceSet.Remove(XMP)
				if err := decodeXMP(io.LimitReader(e.r, int64(boxSize-16)), e.opts); err != nil {
					return err
				}
			}
		}

		// Make sure we put the read pointer at the start of the next box.
		if ftypSize > 0 {
			// Some other box, ignore it.
			e.seek(boxStart + int64(boxSize))
		}
	}

	return nil
}
