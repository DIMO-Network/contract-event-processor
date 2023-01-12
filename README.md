# contract-event-stream

## Overview

This service allows users to receive events from DIMO contracts. The content of specified events are streamed, in order, to a Kafka topic.
The Kafka topic can be set in settings.yaml.
The default number of block confirmations used for this service is 5. This number can be changed in settings.yaml.

## Getting Set Up

1. If you don't already have an API key, you can request one [here](https://docs.alchemy.com/docs/alchemy-quickstart-guide)
2. Create a `settings.yaml` file; be sure to add your Alchemy API key requested in the step above.

   `cp sample.settings.yaml settings.yaml`

## Local Deploy

1. `docker compose up -d`
2. Create a table to track which blocks have already been processed.

   `go run ./cmd migrate`

3. The following will either resume indexing where the process left off during the last run. Or, if the table in the above step is empty, start with the most recently confirmed block (determined by current head minus 5).

   `go run ./cmd`

4. To start processing from a specific block, run the following where BLOCKNUM is the block number you would like to start from:

   `go run ./cmd override BLOCKNUM`

   example: `go run ./cmd override 37948118`

## Events

[Events](https://github.com/DIMO-Network/contract-event-stream/blob/main/cmd/kafka_service.go#L7-L14) are wrapped in a [CloudEvent](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md#example) and include information on the event contract, transaction hash and arguments as well as the event signature and event name. When a block has finished processing a final [Block Completed event](https://github.com/DIMO-Network/contract-event-stream/blob/main/cmd/block_listener.go#L313-L319) is sent to the stream.

```sh
event := shared.CloudEvent[Event]{
# Unique identifier for each event
ID:      ksuid.New().String(),
# Block number being processed
Source:  head.Number.String(),
# Block transaction hash
Subject: vLog.TxHash.String(),
# Time block was mined
Time:    tm,
Data: Event{
    # Contract address of event
    Contract: vLog.Address.String(),
    # Event transaction hash
    TransactionHash: vLog.TxHash.String(),
    # Event signature
    EventSignature:  vLog.Topics[0].String(),
    # Event name
    EventName: ev.Name,
    # Any arguments passed to event
    Arguments: make(map[string]any),
}}
```
