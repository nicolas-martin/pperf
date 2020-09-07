package pperf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

func logSendErr(c net.Conn, err error) error {
	log.Println(err)
	sendErr(c, err)
	return err
}

type stat struct {
	total uint64
	elms  uint64 // elapsed milliseconds
	bps   uint64 // bytes per second
}

type received struct {
	hdr   header
	data  []byte
	start time.Time
	secs  int
	recv  stat
	send  stat
}

// bytes per second
func bps(total uint64, elms uint64) uint64 {
	if elms == 0 {
		return 0
	}
	return 1000 * total / elms
}

// mega-bits-per-second
func mbps(bps uint64) float64 {
	return float64(bps) * 0.000008
}

func readex(c net.Conn, size int) ([]byte, error) {
	buf := make([]byte, size)
	have, rErr := c.Read(buf)
	if rErr != nil {
		return nil, rErr
	}
	for have < size {
		need := size - have
		extra := make([]byte, need)
		add, aErr := c.Read(extra)
		if aErr != nil {
			return nil, aErr
		}
		buf = append(buf[:have], extra[:add]...)
		have += add
	}

	/*
		prior recursive version for reference:
		if have < size {
			extra, xErr := readex(c, size-have)
			if xErr != nil {
				return nil, xErr
			}
			buf = append(buf[:got], extra...)
		}
	*/
	return buf, nil
}

func receive(c net.Conn, r *received) error {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))

	hbuf, rErr := readex(c, headerSize) //c.Read(hbuf)
	if rErr != nil {
		return logSendErr(c, fmt.Errorf("read header: %w", rErr))
	}
	hrErr := binary.Read(bytes.NewReader(hbuf), binary.BigEndian, &r.hdr)
	if hrErr != nil {
		return logSendErr(c, fmt.Errorf("convert header: %w: buf=%+v", hrErr, hbuf))
	}
	if r.hdr.Magic != MAGIC {
		return logSendErr(c, errors.New("received message with wrong magic code"))
	}
	if int(r.hdr.Size) < headerSize || int(r.hdr.Size) > BLOCKSIZE {
		return logSendErr(c, errors.New("received message with invalid header size"))
	}

	dsize := int(r.hdr.Size) - headerSize
	if dsize > 0 {
		dbuf, dErr := readex(c, dsize)
		if dErr != nil {
			return logSendErr(c, fmt.Errorf("read data: %w", dErr))
		}
		if r.hdr.Crc != crc16(dbuf) {
			return logSendErr(c, errors.New("received wrong data crc"))
		}
		r.data = dbuf
	}

	// update calculations
	if r.hdr.Count == 0 {
		// reset values -- first packet doesn't count
		r.start = time.Now()
		r.recv.total = 0
		r.recv.elms = 0
		r.recv.bps = 0
	} else {
		r.secs = int(time.Now().Sub(r.start).Seconds())

		r.recv.elms = uint64(time.Now().Sub(r.start).Milliseconds())
		r.recv.total += uint64(r.hdr.Size)
		r.recv.bps = bps(r.recv.total, r.recv.elms)

		r.send.elms = r.hdr.Elapsed
		r.send.total = r.hdr.Total
		r.send.bps = bps(r.send.total, r.send.elms)
	}
	return nil
}
