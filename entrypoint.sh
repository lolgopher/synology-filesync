#!/bin/sh

set
FLAG=""

if [ ! -z "$SYNOLOGY_IP" ] && [ "$SYNOLOGY_IP" != "" ]; then FLAG="$FLAG --synoip=$SYNOLOGY_IP"; fi
if [ ! -z "$SYNOLOGY_PORT" ] && [ "$SYNOLOGY_PORT" != "" ]; then FLAG="$FLAG --synoport=$SYNOLOGY_PORT"; fi
if [ ! -z "$SYNOLOGY_ID" ] && [ "$SYNOLOGY_ID" != "" ]; then FLAG="$FLAG --synoid=$SYNOLOGY_ID"; fi
if [ ! -z "$SYNOLOGY_PW" ] && [ "$SYNOLOGY_PW" != "" ]; then FLAG="$FLAG --synopw=$SYNOLOGY_PW"; fi
if [ ! -z "$SYNOLGOY_PATH" ] && [ "$SYNOLGOY_PATH" != "" ]; then FLAG="$FLAG --synopath=$SYNOLGOY_PATH"; fi

if [ ! -z "$REMOTE_IP" ] && [ "$REMOTE_IP" != "" ]; then FLAG="$FLAG --remoteip=$REMOTE_IP"; fi
if [ ! -z "$REMOTE_PORT" ] && [ "$REMOTE_PORT" != "" ]; then FLAG="$FLAG --remoteport=$REMOTE_PORT"; fi
if [ ! -z "$REMOTE_ID" ] && [ "$REMOTE_ID" != "" ]; then FLAG="$FLAG --remoteid=$REMOTE_ID"; fi
if [ ! -z "$REMOTE_PW" ] && [ "$REMOTE_PW" != "" ]; then FLAG="$FLAG --remotepw=$REMOTE_PW"; fi
if [ ! -z "$REMOTE_PATH" ] && [ "$REMOTE_PATH" != "" ]; then FLAG="$FLAG --remotepath=$REMOTE_PATH"; fi

if [ ! -z "$LOCAL_PATH" ] && [ "$LOCAL_PATH" != "" ]; then FLAG="$FLAG --localpath=$LOCAL_PATH"; fi

echo ./app $FLAG

# flag 값으로 환경 변수 사용
./app $FLAG