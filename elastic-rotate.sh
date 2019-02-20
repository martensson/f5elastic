#!/bin/bash

# Activate template first if not done.
# curl -XPUT localhost:9200/_template/f5 -H 'Content-Type: application/json' -d@f5-template.json

# Create daily index.
curl -XPUT "http://localhost:9200/f5-`date +%Y-%m-%d`"
# Point the alias f5-today to our newly created index.
curl -XPOST 'http://localhost:9200/_aliases' -H 'Content-Type: application/json' -d '
{
    "actions" : [
        { "remove" : { "index" : "*", "alias" : "f5-today" } },
        { "add" : { "index" : '"\"f5-`date +%Y-%m-%d`\""', "alias" : "f5-today" } }
    ]
}'
