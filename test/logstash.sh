docker run -it -d --name logstash -p 12201:12201/udp logstash:5.2.0 \
    -e 'input { gelf { port => "12201" } } output { stdout {} }'