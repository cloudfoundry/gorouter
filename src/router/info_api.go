package router

import (
	"net/http"
	"encoding/json"
	"fmt"
)

type InfoHandler struct {
	addr string
	m json.Marshaler
}

func (h InfoHandler) ServeHTTP(writer http.ResponseWriter, reader *http.Request) {
	writer.Header().Set("Content-Type", "application/json")

	str, err := json.Marshal(h.m)
	if err != nil {
		errmsg := fmt.Sprintf("InfoHandler.ServeHTTP json.Marshal: %s", err)
		log.Warnf(errmsg)
		http.Error(writer, errmsg, http.StatusInternalServerError)
		return
	}
	writer.Write(str)
}

func (h InfoHandler) Run() {
	s := &http.Server{
		Addr:    h.addr,
		Handler: h,
	}
	log.Infof("Listening for info requests on %s", s.Addr)
	s.ListenAndServe()
}