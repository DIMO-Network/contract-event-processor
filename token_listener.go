package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

type TokenContractListener struct {
	ABI                    *abi.ABI
	Logger                 *zerolog.Logger
	Events                 map[string]abi.Event
	Address                string
	Client                 *ethclient.Client
	AdminChangedEvent      abi.Event
	TokensTransferredEvent abi.Event
	AdminWithdrawalEvent   abi.Event
	RoleGrantedEvent       abi.Event
	RoleAdminChangedEvent  abi.Event
	RoleRevokedEvent       abi.Event
	UpgradedEvent          abi.Event
	BeaconUpgradedEvent    abi.Event
	DidntQualifyEvent      abi.Event
	InitializedEvent       abi.Event
}

func NewTokenContractListener(contractAddress string, ethClient *ethclient.Client) (RewardContractListener, error) {
	abi, err := TokenMetaData.GetAbi()
	if err != nil {
		return RewardContractListener{}, err
	}

	for _, event := range abi.Events {
		fmt.Println(event.Name)
	}

	return RewardContractListener{
		ABI:     abi,
		Address: contractAddress,
		Client:  ethClient,
		// // Logger:                 *zerolog.Logger,
		// AdminChangedEvent:      abi.Events["AdminChanged"],
		// TokensTransferredEvent: abi.Events["TokensTransferred"],
		// AdminWithdrawalEvent:   abi.Events["AdminWithdrawal"],
		// RoleGrantedEvent:       abi.Events["RoleGranted"],
		// RoleAdminChangedEvent:  abi.Events["RoleAdminChanged"],
		// RoleRevokedEvent:       abi.Events["RoleRevoked"],
		// UpgradedEvent:          abi.Events["Upgraded"],
		// BeaconUpgradedEvent:    abi.Events["BeaconUpgraded"],
		// DidntQualifyEvent:      abi.Events["DidntQualify"],
		// InitializedEvent:       abi.Events["Initialized"],
	}, nil

}

func (tcl TokenContractListener) TokenContractListener() {
	fmt.Println("\tListening To: ", tcl.Address)
	query := ethereum.FilterQuery{
		Addresses: []common.Address{common.HexToAddress(tcl.Address)},
	}

	logs := make(chan types.Log)

	sub, err := tcl.Client.SubscribeFilterLogs(context.Background(), query, logs)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Running...")
	for {
		select {
		case err := <-sub.Err():
			log.Fatal(err, 3)
		case vLog := <-logs:
			switch vLog.Topics[0] {
			case tcl.AdminChangedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.AdminChangedEvent.Name)
			case tcl.TokensTransferredEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.TokensTransferredEvent.Name)
			case tcl.AdminWithdrawalEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.AdminWithdrawalEvent.Name)
			case tcl.RoleGrantedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.RoleGrantedEvent.Name)
			case tcl.RoleAdminChangedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.RoleAdminChangedEvent.Name)
			case tcl.RoleRevokedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.RoleRevokedEvent.Name)
			case tcl.UpgradedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.UpgradedEvent.Name)
			case tcl.BeaconUpgradedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.BeaconUpgradedEvent.Name)
			case tcl.DidntQualifyEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.DidntQualifyEvent.Name)
			case tcl.InitializedEvent.ID:
				// send event to kafka channel
				fmt.Printf("\tContract: %v Event: %v", tcl.Address, tcl.InitializedEvent.Name)
			}

		}
	}

}
