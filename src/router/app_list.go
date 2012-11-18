package router

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"sync"
)

type AppList struct {
	sync.Mutex
	appMap map[string]bool
}

func NewAppList() *AppList {
	list := new(AppList)
	list.appMap = make(map[string]bool)

	return list
}

func (l *AppList) Contain(app string) bool {
	return l.appMap[app]
}

func (l *AppList) Size() int {
	return len(l.appMap)
}

func (l *AppList) Insert(app string) {
	l.Lock()
	defer l.Unlock()

	l.insert(app)
}

func (l *AppList) insert(app string) {
	l.appMap[app] = true
}

func (l *AppList) Remove(app string) {
	l.Lock()
	defer l.Unlock()

	l.remove(app)
}

func (l *AppList) remove(app string) {
	delete(l.appMap, app)
}

func (l *AppList) Reset() {
	l.Lock()
	defer l.Unlock()

	l.reset()
}

func (l *AppList) reset() {
	l.appMap = make(map[string]bool)
}

func (l *AppList) MarshalJSON() ([]byte, error) {
	slice := make([]string, 0, len(l.appMap))

	l.Lock()
	defer l.Unlock()

	for app, _ := range l.appMap {
		slice = append(slice, app)
	}

	return json.Marshal(slice)
}

func (l *AppList) UnmarshalJSON(data []byte) error {
	slice := make([]string, 0)
	err := json.Unmarshal(data, &slice)
	if err != nil {
		return err
	}

	l.Lock()
	defer l.Unlock()

	l.appMap = make(map[string]bool)

	for _, app := range slice {
		l.appMap[app] = true
	}

	return nil
}

func (l *AppList) Encode() (out []byte, err error) {
	out, err = json.Marshal(l)
	if err != nil {
		return
	}

	var b bytes.Buffer
	writer := zlib.NewWriter(&b)
	writer.Write(out)
	writer.Close()

	out = b.Bytes()
	return
}

func (l *AppList) EncodeAndReset() ([]byte, error) {
	// Here we create a readonly snapshot of AppList l2. And use l2 to do the
	// encoding work. At the same time, reset AppList l and release the lock,
	// so we the operations on l are not blocked.
	var l2 AppList

	l.Lock()
	l2.appMap = l.appMap
	l.appMap = make(map[string]bool)
	l.Unlock()

	return l2.Encode()
}

func DecodeAppList(code []byte) (appList *AppList, err error) {
	b := bytes.NewBuffer(code)

	var reader io.Reader
	reader, err = zlib.NewReader(b)
	if err != nil {
		return
	}

	var list []string = make([]string, 0)
	decoder := json.NewDecoder(reader)
	err = decoder.Decode(&list)
	if err != nil {
		return
	}

	appList = NewAppList()
	for _, app := range list {
		appList.insert(app)
	}

	return
}
