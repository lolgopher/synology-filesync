#!/bin/sh

set
FLAG=""

if [ ! -z "$CONFIG_PATH" ] && [ "$CONFIG_PATH" != "" ]; then FLAG="$FLAG --config=$CONFIG_PATH"; fi

echo ./app $FLAG

# flag 값으로 환경 변수 사용
./app $FLAG