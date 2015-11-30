#!/bin/sh

# Copy existing configuration if pre-existing installation is found
test -f /etc/opt/influxdb/influxdb.conf
if [ $? -eq 0 ]; then
    # Create new configuration location (if not already there)
    test -d /etc/influxdb || mkdir -p /etc/influxdb
    
    # Do not overwrite configuration, if already exists
    test -f /etc/influxdb/influxdb.conf
    if [ $? -ne 0 ]; then
	# Configuration does not exist, copy (and backup, just in case)
	cp --backup --suffix=.$(date +%s).install-backup -a /etc/opt/influxdb/* /etc/influxdb/
    fi
fi

exit
