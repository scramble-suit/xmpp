// Copyright 2016 Sam Whited.
// Use of this source code is governed by the BSD 2-clause license that can be
// found in the LICENSE file.

package compress

import (
	"compress/zlib"
	"io"
	"sync"
)

// Method is a stream compression method. Custom methods may be defined, but
// generally speaking the only supported methods will be those with names
// defined in the "Stream Compression Methods Registry" maintained by the XSF
// Editor: https://xmpp.org/registrar/compress.html
type Method struct {
	Name    string
	Wrapper func(io.ReadWriteCloser) (io.ReadWriteCloser, error)
}

type multiCloser []io.Closer

// Close attempts to call every close method in the multiCloser. It always
// attempts all of them (unless one of them panics), but it only returns the
// last error if multiple of them error. There's probably a better way.
func (mc multiCloser) Close() (err error) {
	var e error
	for _, c := range mc {
		// Return one error; we don't really care what.
		if e = c.Close(); e != nil {
			err = e
		}
	}
	return err
}

// zlibDelayedSetup is an io.ReadWriteCloser that uses an underlying zlib reader
// and writer, but defers creation of the reader until the first read. It is a
// jank hack to work around the fact that the zlib reader tries to immediately
// read header data from the connection (and blocks until it can do so), but if
// we're a client we need to go ahead and return the new reader and send a
// <stream:stream> before reading the header data.
type zlibDelayedSetup struct {
	wm, rm sync.Mutex

	raw        io.ReadWriteCloser
	zlibWriter *zlib.Writer
	zlibReader io.ReadCloser
}

func (r *zlibDelayedSetup) readSetup() (err error) {
	r.rm.Lock()
	defer r.rm.Unlock()

	if r.zlibReader == nil {
		if r.zlibReader, err = zlib.NewReader(r.raw); err != nil {
			return
		}
	}

	return
}

func (r *zlibDelayedSetup) Write(p []byte) (n int, err error) {
	if n, err = r.zlibWriter.Write(p); err != nil {
		return
	}
	return n, r.zlibWriter.Flush()
}

func (r *zlibDelayedSetup) Read(p []byte) (n int, err error) {
	if err = r.readSetup(); err != nil {
		return
	}
	return r.zlibReader.Read(p)
}

func (r *zlibDelayedSetup) Close() error {

	mc := multiCloser{r.raw}

	r.rm.Lock()
	defer r.rm.Unlock()
	if r.zlibReader != nil {
		mc = append(mc, r.zlibReader)
	}

	r.wm.Lock()
	defer r.wm.Unlock()
	if r.zlibWriter != nil {
		mc = append(mc, r.zlibWriter)
	}

	return mc.Close()
}

var zlibMethod = Method{
	Name: "zlib",
	Wrapper: func(rwc io.ReadWriteCloser) (io.ReadWriteCloser, error) {
		return &zlibDelayedSetup{raw: rwc, zlibWriter: zlib.NewWriter(rwc)}, nil
	},
}
