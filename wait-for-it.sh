#!/usr/bin/env bash
# Use this script to test if a given TCP host/port are available

TIMEOUT=15
QUIET=0
HOST=""
PORT=""

echoerr() {
    if [ "$QUIET" -ne 1 ]; then echo "$@" 1>&2; fi
}

usage() {
    cat << USAGE >&2
Usage:
    $0 host:port [-t timeout] [-- command args]
    -q | --quiet                        Do not output any status messages
    -t TIMEOUT | --timeout=timeout      Timeout in seconds, zero for no timeout
    -- COMMAND ARGS                     Execute command with args after the test finishes
USAGE
    exit 1
}

wait_for() {
    if [ "$TIMEOUT" -gt 0 ]; then
        echoerr "$0: waiting $TIMEOUT seconds for $HOST:$PORT"
    else
        echoerr "$0: waiting for $HOST:$PORT without a timeout"
    fi
    start_ts=$(date +%s)
    while :
    do
        if [ "$ISBUSY" -eq 1 ]; then
            nc -z $HOST $PORT
            result=$?
        else
            (echo > /dev/tcp/$HOST/$PORT) >/dev/null 2>&1
            result=$?
        fi
        if [ $result -eq 0 ]; then
            end_ts=$(date +%s)
            echoerr "$0: $HOST:$PORT is available after $((end_ts - start_ts)) seconds"
            break
        fi
        sleep 1
    done
    return $result
}

wait_for_wrapper() {
    # In order to support SIGINT during timeout: http://unix.stackexchange.com/a/57692
    if [ "$QUIET" -eq 1 ]; then
        timeout $TIMEOUT $0 $HOST:$PORT -q -t 0 -- "$CMD" &
    else
        timeout $TIMEOUT $0 $HOST:$PORT -t 0 -- "$CMD" &
    fi
    pid=$!
    trap "kill -INT -$pid" INT
    wait $pid
    result=$?
    if [ $result -ne 0 ]; then
        echoerr "$0: timeout occurred after waiting $TIMEOUT seconds for $HOST:$PORT"
    fi
    return $result
}

while [ $# -gt 0 ]
do
    case "$1" in
        *:* )
        HOST=$(printf "%s\n" "$1"| cut -d : -f 1)
        PORT=$(printf "%s\n" "$1"| cut -d : -f 2)
        shift 1
        ;;
        -q | --quiet)
        QUIET=1
        shift 1
        ;;
        -t)
        TIMEOUT="$2"
        if [ "$TIMEOUT" = "" ]; then break; fi
        shift 2
        ;;
        --timeout=*)
        TIMEOUT="${1#*=}"
        shift 1
        ;;
        --)
        shift
        CMD="$@"
        break
        ;;
        --help)
        usage
        ;;
        *)
        echoerr "Unknown argument: $1"
        usage
        ;;
    esac
done

if [ "$HOST" = "" ] || [ "$PORT" = "" ]; then
    echoerr "Error: you need to provide a host and port to test."
    usage
fi

ISBUSY=$(which nc)
if [ "$ISBUSY" = "" ]; then
    ISBUSY=$(which busybox)
    if [ "$ISBUSY" != "" ]; then
        ISBUSY=1
    else
        ISBUSY=0
    fi
else
    ISBUSY=1
fi

if [ "$TIMEOUT" -gt 0 ]; then
    wait_for_wrapper
else
    wait_for
fi

result=$?
if [ "$CMD" != "" ]; then
    exec $CMD
else
    exit $result
fi