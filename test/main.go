package main

import (
	"errors"

	graylog "github.com/johnnyluo/logrus-graylog-hook"
	log "github.com/sirupsen/logrus"
)

func main() {
	h := graylog.NewGraylogHook("127.0.0.1:12201")
	log.AddHook(h)
	defer h.Flush()
	for i := 0; i < 100; i++ {
		log.Infof("HelloWorld:%d", i)
	}
	for i := 0; i < 100; i++ {
		log.WithError(errors.New("my error")).Infoln("error")
	}
}
