/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

package native

import (
	"fmt"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/errors"
	"github.com/ontio/ontology/smartcontract/context"
	"github.com/ontio/ontology/smartcontract/event"
	"github.com/ontio/ontology/smartcontract/states"
	sstates "github.com/ontio/ontology/smartcontract/states"
	"github.com/ontio/ontology/smartcontract/storage"
)

type (
	// 内置合约的 各个func类型
	Handler         func(native *NativeService) ([]byte, error)
	// 注册内置合约的 func
	RegisterService func(native *NativeService)
)

var (
	// todo 这里头装的就是 本体所有系统合约的注册func而已
	Contracts = make(map[common.Address]RegisterService)
)

// Native service struct
// Invoke a native smart contract, new a native service
type NativeService struct {
	CacheDB       *storage.CacheDB

	// todo 这里头装的都是系统合约
	ServiceMap    map[string]Handler
	Notifications []*event.NotifyEventInfo
	InvokeParam   sstates.ContractInvokeParam
	Input         []byte
	Tx            *types.Transaction
	Height        uint32
	Time          uint32
	BlockHash     common.Uint256
	ContextRef    context.ContextRef
	PreExec       bool
}

func (this *NativeService) Register(methodName string, handler Handler) {
	this.ServiceMap[methodName] = handler
}


/**
todo 系统合约的执行入口。
 */
func (this *NativeService) Invoke() ([]byte, error) {

	// todo 取出合约上下文
	contract := this.InvokeParam
	services, ok := Contracts[contract.Address]
	if !ok {
		return BYTE_FALSE, fmt.Errorf("Native contract address %x haven't been registered.", contract.Address)
	}
	services(this)
	/**
	todo 根据对应的系统合约方法名 取出 系统合约func
	 */
	service, ok := this.ServiceMap[contract.Method]
	if !ok {
		return BYTE_FALSE, fmt.Errorf("Native contract %x doesn't support this function %s.",
			contract.Address, contract.Method)
	}
	args := this.Input
	this.Input = contract.Args
	this.ContextRef.PushContext(&context.Context{ContractAddress: contract.Address})

	// 处理系统合约 event 相关 todo <其实系统合约没有 event>
	notifications := this.Notifications
	this.Notifications = []*event.NotifyEventInfo{}

	// todo 执行对应的系统合约
	result, err := service(this)
	if err != nil {
		return result, errors.NewDetailErr(err, errors.ErrNoCode, "[Invoke] Native serivce function execute error!")
	}
	this.ContextRef.PopContext()

	// 压入系统合约调用事件  todo <其实系统合约是没有event的，所以压了个空的>
	this.ContextRef.PushNotifications(this.Notifications)
	this.Notifications = notifications
	this.Input = args
	return result, nil
}

// todo 开始执行本地调用， 即： 调用系统合约
func (this *NativeService) NativeCall(address common.Address, method string, args []byte) (interface{}, error) {

	// todo 根据 系统合约的账户 addr 和本地需要调用的 method名称 和 参数args 初始化一个 合约上下文
	c := states.ContractInvokeParam{
		Address: address,
		Method:  method,
		Args:    args,
	}
	this.InvokeParam = c

	// 调用它
	return this.Invoke()
}
