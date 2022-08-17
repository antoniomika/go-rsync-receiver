package rsyncreceiver

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"log"

	"github.com/antoniomika/go-rsync-receiver/rsync"
	"github.com/mmcloughlin/md4"
)

// rsync/receiver.c:recv_files
func (rt *recvTransfer) recvFiles(fileList []*file) error {
	phase := 0
	for {
		idx, err := rt.conn.ReadInt32()
		if err != nil {
			return err
		}
		if idx == -1 {
			if phase == 0 {
				phase++
				log.Printf("recvFiles phase=%d", phase)
				// TODO: send done message
				continue
			}
			break
		}
		log.Printf("receiving file idx=%d: %+v", idx, fileList[idx])
		if err := rt.recvFile1(fileList[idx]); err != nil {
			return err
		}
	}
	log.Printf("recvFiles finished")
	return nil
}

func (rt *recvTransfer) recvFile1(f *file) error {
	if err := rt.receiveData(f); err != nil {
		return err
	}
	return nil
}

// rsync/receiver.c:receive_data
func (rt *recvTransfer) receiveData(f *file) error {
	f.Buf = bytes.NewBuffer(nil)

	var sh rsync.SumHead
	if err := sh.ReadFrom(rt.conn); err != nil {
		return err
	}

	h := md4.New()
	binary.Write(h, binary.LittleEndian, rt.seed)

	wr := io.MultiWriter(f.Buf, h)

	for {
		token, data, err := rt.recvToken()
		if err != nil {
			return err
		}
		if token == 0 {
			break
		}
		if token > 0 {
			if _, err := wr.Write(data); err != nil {
				return err
			}
			continue
		}
		token = -(token + 1)
		offset2 := int64(token) * int64(sh.BlockLength)
		dataLen := sh.BlockLength
		if token == sh.ChecksumCount-1 && sh.RemainderLength != 0 {
			dataLen = sh.RemainderLength
		}
		data = make([]byte, dataLen)
		if _, err := bytes.NewReader(f.Buf.Bytes()).ReadAt(data, offset2); err != nil {
			return err
		}

		if _, err := wr.Write(data); err != nil {
			return err
		}
	}
	localSum := h.Sum(nil)
	remoteSum := make([]byte, len(localSum))
	if _, err := io.ReadFull(rt.conn.Reader, remoteSum); err != nil {
		return err
	}
	if !bytes.Equal(localSum, remoteSum) {
		return fmt.Errorf("file corruption in %s", f.Name)
	}
	log.Printf("checksum %x matches!", localSum)

	return nil
}