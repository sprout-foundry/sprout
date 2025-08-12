#!/bin/bash

if [ "$1" == "--single" ]; then
    python3 test_runner.py --single
else 
    python3 test_runner.py
fi
