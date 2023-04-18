# synology-filesync

Synology File Station API를 이용해 파일을 다운로드 받고, Remote Server에 전송합니다.

<img alt="Github go.mod Go Version" src="https://img.shields.io/github/go-mod/go-version/lolgopher/synology-filesync">
<img alt="GitHub Workflow Status" src="https://img.shields.io/github/actions/workflow/status/lolgopher/synology-filesync/ci.yaml">
<a href="https://goreportcard.com/report/github.com/lolgopher/synology-filesync"><img src="https://goreportcard.com/badge/github.com/lolgopher/synology-filesync" alt="Go Report Card"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License: MIT"></a>

## 프로젝트 개요

Synology의 특정 경로의 파일들을 다른 서버에 지속적으로 동기화 시키고 싶을 때 사용할 수 있습니다.

[Synology File Station API](https://global.download.synology.com/download/Document/Software/DeveloperGuide/Package/FileStation/All/enu/Synology_File_Station_API_Guide.pdf) 를 이용해 파일을 다운로드 합니다.

SFTP를 사용해 다른 서버에 파일을 전송합니다.

각 경로의 metadata.yaml 파일을 참고하여 파일 전송 여부를 확인합니다.

[Pixelify-Google-Photos](https://github.com/BaltiApps/Pixelify-Google-Photos)와 해당 프로젝트를 사용해 Google Photo에 무제한 백업을 중계하는 파일 리시버 서버로 활용할 수 있습니다.

## 빠른 시작

[릴리즈 페이지](https://github.com/lolgopher/synology-filesync/releases)에서 최신 버전을 다운로드 받아 실행하세요.

```
Usage of ./synology-filesync:
  -localpath string
        Local path to save download files
  -remoteid string
        Remote SSH username
  -remoteip string
        Remote SSH IP address
  -remotepath string
        Remote path to download files
  -remoteport string
        Remote SSH port
  -remotepw string
        Remote SSH password
  -synoid string
        FileStation account username
  -synoip string
        FileStation IP address
  -synopath string
        FileStation path to download files
  -synoport string
        FileStation port
  -synopw string
        FileStation account password
  -v    Show version
```

## 빌드 방법

### 바이너리

1. 소스코드 가져오기

    ```shell
    git clone https://github.com/lolgopher/synology-filesync
    cd synology-filesync
    ```
   
2. 바이너리 빌드

   ```shell
   go build
   ```

3. 실행
   ```shell
   ./synology-filesync
   ```

### Docker

1. 소스코드 가져오기

    ```shell
    git clone https://github.com/lolgopher/synology-filesync
    cd synology-filesync
    ```

2. Docker 이미지 빌드

   ```shell
   docker build -t synology-filesync .
   ```

3. Docker 컨테이너 실행

   ```shell
   docker run --rm -e SYNOLOGY_IP="" synology-filesync   
   ```
   

## 기여 방법

이 프로젝트에 대한 기여는 언제나 환영합니다! 기여 방법은 다음과 같습니다.

1. 이 저장소를 포크(fork)합니다.
2. 새 브랜치(branch)를 만듭니다: `git checkout -b topic-new_feature_branch`.
3. 변경 사항을 커밋(commit)합니다: `git commit -am 'add some feature'`.
4. 브랜치에 푸시(push)합니다: `git push origin topic-new_feature_branch`.
5. 풀 리퀘스트(Pull Request)를 작성합니다.

의견이나 문제가 있으면 Issues를 열어주십시오.

감사합니다!

## 라이선스

[MIT License](https://github.com/lolgopher/synology-filesync/blob/master/LICENSE)