# PREREQUISITES

Для корректной работы приложения в ситуации, когда сетевой диск примонтирован к файловой системе, необходимо,   
чтобы у пользователя, который будет запускать утилиту для переноса артифактов,  
были права на чтение/запись в указанной директории.  
Проверить это можно с помощью специальных endpoint'ов ```/check-nfs-read```, ```/check-nfs-write```.  
Если таких прав нет или не хочется разбираться, утилиту fts-cd следует запускать под root'ом.  

Для корректной работы приложения необходимо, чтобы корректно работал docker.
Для этого нужно убедиться, что у пользователя, который будет запускать утилиту для переноса артифактов,  
были права на запуск команды ```docker run hello-world```.  
Если таких прав нет или не хочется разбираться, утилиту fts-cd следует запускать под root'ом.  

Как правильно примонтировать диск и настроить докер можно почитать в Confluence.  
https://sdlc.go.rshbank.ru/confluence/pages/viewpage.action?pageId=308683890

# make file executable

chmod o+x fts-cd

# run send-mode

./fts-cd -config=./send-config.json

# run receive-mode

./fts-cd -config=./receive-config.json

# send transfer-docker-artifact-request 

curl -X POST -H 'Content-Type: application/json' -d '{"artifact":"alpine"}' http://localhost:8080/cd-docker-start

# send transfer-docker-artifact-request with specified jobId

curl -X POST -H 'Content-Type: application/json' -d '{"artifact":"alpine"}' http://localhost:8080/cd-docker-start/0001

# send transfer-pypi-artifact-request 

curl -X POST -H 'Content-Type: application/json' -d '{"package":"hello-world-package", "version":"0.1.3"}' http://localhost:8080/cd-pypi-start

# check status
curl http://localhost:8080/cd-ping/latest

# check status
curl http://localhost:8080/cd-ping/0001