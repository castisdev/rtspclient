# rtspclient

기본
```bash
$ ./rtspclient -url rtsp://172.16.11.100:8554 -transport TCP
```
\
100개 세션
```bash
$ ./rtspclient -url rtsp://172.16.11.100:8554 -transport TCP -count 100
```
\
rtsp://172.16.11.100:8554/10001.stream ~ rtsp://172.16.11.100:8554/10002.stream
```bash
$ ./rtspclient -url rtsp://172.16.11.100:8554/{NUM}.stream -transport TCP -start 10001 -end 10002
```