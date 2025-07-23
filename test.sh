#!/bin/bash

if [ "$1" == "--single" ]; then
    python3 test.py --single
else 
    python3 test.py
fi
