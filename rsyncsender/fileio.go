package rsyncsender

import (
	"bytes"
	"io"
)

// rsync.h:map_struct
type mapStruct struct {
	fileSize      int64         // file size (from stat)
	pOffset       int64         // window start
	pFdOffset     int64         // offset of cursor in fd ala lseek
	window        []byte        // window pointer
	pSize         int64         // largest window we allocated
	pLen          int64         // latest (rounded) window size
	defWindowSize int64         // default window size
	f             *bytes.Reader // file descriptor
	err           error         // first read error
}

const alignBoundary = 1024

func alignedLength(l int64) int64 {
	return ((l - 1) | (alignBoundary - 1)) + 1
}

func alignedOvershoot(off int64) int64 {
	return off & (alignBoundary - 1)
}

func mapFile(f *bytes.Reader, len int64, readSize int32, blkSize int32) *mapStruct {
	if blkSize > 0 && readSize%blkSize != 0 {
		readSize += blkSize - (readSize % blkSize)
	}
	return &mapStruct{
		fileSize:      len,
		defWindowSize: alignedLength(int64(readSize)),
		f:             f,
	}
}

func (ms *mapStruct) ptr(offset int64, l int32) []byte {
	len := int64(l)
	if len == 0 || len < 0 {
		return nil
	}

	if offset >= ms.pOffset && offset+int64(len) <= ms.pOffset+int64(ms.pLen) {
		// region already available
		off := offset - ms.pOffset
		return ms.window[off : off+int64(len)]
	}

	alignFudge := alignedOvershoot(offset)
	windowStart := offset - alignFudge
	windowSize := int64(ms.defWindowSize)
	if windowStart+windowSize > ms.fileSize {
		windowSize = ms.fileSize - windowStart
	}
	if windowSize < len+alignFudge {
		windowSize = alignedLength(len + alignFudge)
	}
	if windowSize > ms.pSize {
		win := make([]byte, windowSize)
		copy(win, ms.window)
		ms.window = win
		ms.pSize = windowSize
	}
	readStart := windowStart
	readSize := windowSize
	readOffset := int64(0)

	if windowStart >= ms.pOffset && windowStart < ms.pOffset+ms.pLen &&
		windowStart+windowSize >= ms.pOffset+ms.pLen {
		readStart = ms.pOffset + ms.pLen
		readOffset = readStart - windowStart
		readSize = windowSize - readOffset
		off := ms.pLen - readOffset
		copy(ms.window[:], ms.window[off:off+readOffset])
	}
	if readSize <= 0 {
		return nil
	}
	if ms.pFdOffset != readStart {
		if _, err := ms.f.Seek(readStart, io.SeekStart); err != nil {
			return nil
		}
		ms.pFdOffset = readStart
	}
	ms.pOffset = windowStart
	ms.pLen = windowSize
	for readSize > 0 {
		n, err := ms.f.Read(ms.window[readOffset : readOffset+readSize])
		if err != nil {
			ms.err = err
			break
		}
		ms.pFdOffset += int64(n)
		readOffset += int64(n)
		readSize -= int64(n)
	}
	return ms.window[alignFudge : alignFudge+len]
}
