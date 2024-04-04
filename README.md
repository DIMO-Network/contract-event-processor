# contract-event-stream

## Overview

This service allows users to receive events from DIMO contracts. The content of specified events are streamed, in order, to a Kafka topic.
The Kafka topic can be set in settings.yaml.
The default number of block confirmations used for this service is 5. This number can be changed in settings.yaml.

## Watching contracts

In addition to the normal settings, the app loads a `config.yaml` file. Here is an example:

```yaml
contracts:
  - address: "0x5FbDB2315678afecb367f032d93F642f64180aa3"
    abi: charts/contract-event-processor/abi/DIMORegistry.json
  - address: "0xc6e7DF5E7b4f2A278906862b61205850344D4e7d"
    abi: charts/contract-event-processor/abi/VehicleId.json
```

## Getting Set Up

1. If you don't already have an API key, you can request one [here](https://docs.alchemy.com/docs/alchemy-quickstart-guide)
2. Create a `settings.yaml` file; be sure to add your Alchemy API key requested in the step above.

   `cp sample.settings.yaml settings.yaml`

## Local Deploy

1.  `docker compose up -d`
2.  Create a table to track which blocks have already been processed.

    `go run ./cmd/contract-event-processor migrate`

3.  The following will either resume indexing where the process left off during the last run. Or, if the table in the above step is empty, start with the most recently confirmed block (determined by current head minus 5).

    `go run ./cmd/contract-event-processor`

4.  To start processing from a specific block, update the `STARTING_BLOCK` parameter in `settings.yaml`:

    example: `go run ./cmd/contract-event-processor`

          settings.yaml: `STARTING_BLOCK: 54534725`

5.  To prevent the event processor from running continuously, pass `limit` as a command line argument:

    example: `go run ./cmd/contract-event-processor --limit 5`

## Events

[Events](https://github.com/DIMO-Network/contract-event-stream/blob/main/cmd/kafka_service.go#L7-L14) are wrapped in a [CloudEvent](https://github.com/cloudevents/spec/blob/v1.0.2/cloudevents/spec.md#example) and include information on the event contract, transaction hash and arguments as well as the event signature and event name. When a block has finished processing a final [Block Completed event](https://github.com/DIMO-Network/contract-event-stream/blob/main/cmd/block_listener.go#L313-L319) is sent to the stream.

```sh
{
  "id": "2KWDh5y3t3420QcQSxclUroHGXH"
# Block number being processed
Source:  head.Number.String(),
# Block or transaction hash vLog.TxHash.String() || blockHash.String()
Subject: vLog.TxHash.String(),
# Time block was mined
Time:    tm,
# type of event: blockcompleted || dimoevent
Type:    "zone.dimo.contract.event",

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

Tweak README
