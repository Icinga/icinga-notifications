package listener

import (
	"encoding/json"
	"fmt"
	"github.com/icinga/noma/internal/event"
	"github.com/icinga/noma/internal/object"
	"log"
	"net/http"
	"time"
)

type Listener struct {
	address string
	mux     http.ServeMux
}

func NewListener(address string) *Listener {
	l := &Listener{address: address}
	l.mux.HandleFunc("/process-event", l.ProcessEvent)
	return l
}

func (l *Listener) Run() error {
	log.Printf("Starting listener on http://%s", l.address)
	return http.ListenAndServe(l.address, &l.mux)
}

func (l *Listener) ProcessEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprintln(w, "POST required")
		return
	}

	var ev event.Event
	err := json.NewDecoder(r.Body).Decode(&ev)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "cannot parse JSON body: %v\n", err)
		return
	}
	ev.Time = time.Now()

	obj := object.FromTags(ev.Tags)
	obj.UpdateMetadata(ev.Source, ev.Name, ev.URL, ev.ExtraTags)

	log.Printf("received:\n\n%s\n%s", obj.String(), ev.String())

	w.WriteHeader(http.StatusTeapot)
	_, _ = fmt.Fprintln(w, "received event")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, obj.String())
	_, _ = fmt.Fprintln(w, ev.String())
}
