# cd-file-utility

Сервис для обеспечения cd-процессов RAISA.  
Используется для встраивания в pipeline'ы RAISA.  
Скачивает файл из Nexus и перекладывает его на сетевую шару.  

Команда запуска приложения:  
```./cd-file-utility -config=/path/to/config.json ```

### Конфигурация 
Конфигурационный файл представляет собой json-файл.  
Ключ `mode` определяет в каком режиме запущено приложение.  
Общие ключи не имеют префикса.  
Ключи, необходимые для запуска в режиме `SEND`, имеют префикс `send_`
Ключи, необходимые для запуска в режиме `RECEIVE`, имеют префикс `receive_`


#### Конфигурация SEND
```
{
    "port": ":8080",  
    "nfs_path": "fs:///home/GO/raisa", 
    "buffer_size": "23KB",
    "mode": "SEND",
    "send_docker_registry": "10.7.86.10:38082",
    "send_docker_registry_login": "raisa",
    "send_docker_registry_password": "Qwerty123",
    "send_nexus_url": "http://10.7.86.10:8081",
    "send_nexus_pypi_repository": "pypi-hosted",
    "send_nexus_login": "raisa",
    "send_nexus_password": "Qwerty123"
}
```
#### Конфигурация RECEIVE
```
{
    "port": ":8081",
    "nfs_path": "smb://truskov-aa@RSHBINTECH:PASSWORD@sgo-fc01-r13.go.rshbank.ru:445/inbox-intech",
    "smb_share_path": "Обмен данными Банк-Интех/Truskov-AA",
    "buffer_size": "5MB",
    "mode": "RECEIVE",
    "send_docker_registry": "10.7.86.10:38082",
    "receive_docker_registry": "10.7.86.10:38082",
    "receive_docker_registry_login": "raisa",
    "receive_docker_registry_password": "Qwerty123",
    "receive_pypi_enabled": true,
    "receive_nexus_url": "http://10.7.86.10:8081",
    "receive_nexus_pypi_repository": "pypi-hosted",
    "receive_nexus_login": "raisa",
    "receive_nexus_password": "Qwerty123"
}
```

#### Поля
* `port` - Порт запуска приложения.
  _Указывается с двоеточием._
* `nfs_path` - Путь к сетевой папке, в которой будет размещаться скачанный артефакт. Имеет формат [URL](https://adam.herokuapp.com/past/2010/3/30/urls_are_the_uniform_way_to_locate_resources/). 
* `smb_share_path` - Путь к папке на сетевом диске, например ```Обмен данными Банк-Интех/Truskov-AA```. Используется только если тип протокола - smb
* `buffer_size` - Размер буфера, который будет использоваться для скачивания. Пример: `32KB`, `10MB`. Значение по умолчанию: `5MB`
* `enable_chunking` - Включает режим фрагментированной передачи больших файлов. Значение по умолчанию: `false`
* `chunk_size` - Размер одного фрагмента при фрагментированной передаче. Пример: `50MB`. Значение по умолчанию: `50MB`
* `chunking_threshold` - Порог размера свободного места на диске, при котором автоматически включается фрагментированная передача. Пример: `100MB`. Значение по умолчанию: `100MB`
* `mode` - Режим, в котором работает приложение. Допустимые значения: `SEND`, `RECEIVE`
* `send_docker_enabled` - feature-toggle для отправки docker-артифактов
* `send_docker_registry` - адрес локального docker registry, из которого будет скачан артефакт. Например, `10.7.86.10:38082`
* `send_docker_registry_login` - логин к docker registry.
* `send_docker_registry_password` - пароль к docker registry.
* `send_nexus_url` - адрес nexus, из которого будет скачан артефакт. Например, `http://10.7.86.10:8081`
* `send_nexus_pypi_repository` - название pypi-репозитория. Например, `pypi-hosted`
* `send_nexus_login` - логин к nexus
* `send_nexus_password` - пароль к nexus
* `receive_docker_enabled` - feature-toggle для загрузки docker-артифактов
* `receive_docker_registry` - адрес локального docker registry, в котором нужно разместить артефакт. Например, `10.7.86.10:38082`
* `receive_docker_registry_login` - логин к docker registry.
* `receive_docker_registry_password` - пароль к docker registry.
* `receive_pypi_enabled` - feature-toggle для загрузки python-артифактов. Проверяет доступность утилиты `twine` при старте приложения.
* `receive_nexus_url` - адрес nexus, из которого будет скачан артефакт. Например, `http://10.7.86.10:8081`
* `receive_nexus_pypi_repository` - название pypi-репозитория. Например, `pypi-hosted`
* `receive_nexus_login` - логин к nexus
* `receive_nexus_password` - пароль к nexus

Если необходимо использовать dockerhub, то поля ```send_docker_registry_login``` и ```send_docker_registry_password``` нужно оставить пустыми.  

### Endpoints

### Common Endpoints

#### GET /
Возвращает статус 200 и конфиг запуска приложения.

#### GET /check-nfs-read
Проверяет доступность nfs-папки на чтение.  
Возвращает список файлов в этой папке.

#### GET /check-nfs-write
Проверяет доступность nfs-папки на запись.  
Пробует записать файл с содержимым `My Content` и затем удалить его.  
Название файла имеет формат `tmp-YYYYMMDDHHmmss.txt`

### Send Endpoints

#### POST /cd-start/:jobId
Запуск cd-пайплайна.  
В пути передаётся уникальный идентификатор, например номер пайплайна.  

#### POST /cd-docker-start/:jobId
Запуск cd-пайплайна для Докера.  
В пути передаётся уникальный идентификатор, например номер пайплайна.  
Тело запроса содержит название артифакта `{"artifact":"alpine"}`  

#### POST /cd-docker-start
Работает идентично **/cd-docker-start/:jobId**.  
jobId формируется автоматически в формате YYYYMMDDHHmmss.   

#### GET /cd-ping/:jobId
Проверяет статус задания.  
Возможные статусы:  
`DOWNLOADING` - идёт загрузка файла  
`DOWNLOADING_FAILED` - загрузка файлов не удалась   
`META_WRITING_FAILED` - запись файла метаданных не удалась   
`DOWNLOADING_DONE` - загрузка файла завершена   
`SUCCESS` - файл успешно размещён

#### GET /cd-ping/latest
Работает идентично **/cd-ping/:jobId**  
Будет возвращён статус последнего запущенного задания.  

### Deploy Endpoints

## Инструкция для DevOps
Перечень prerequisites для запуска программы и последовательность команд можно найти в [devops-readme.md](devops-readme.md) 

## Сборка под разные платформы
Собрать приложение для текущей платформы можно командой ```go build -o fts-cd```

Собрать приложение под Linux на Windows из командной строки можно запустив `go-build-linux.cmd` под правами администратора.

Собрать приложение под Linux на Windows из Idea можно изменив значения переменных _GOOS_ и _GOARCH_ в **Languages & Frameworks > Go > Build Tags**