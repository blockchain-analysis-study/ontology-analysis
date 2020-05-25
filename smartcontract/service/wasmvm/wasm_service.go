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
package wasmvm

import (
	"github.com/go-interpreter/wagon/exec"
	"github.com/hashicorp/golang-lru"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/core/store"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/errors"
	"github.com/ontio/ontology/smartcontract/context"
	"github.com/ontio/ontology/smartcontract/event"
	"github.com/ontio/ontology/smartcontract/states"
	"github.com/ontio/ontology/smartcontract/storage"
)

type WasmVmService struct {
	Store         store.LedgerStore
	CacheDB       *storage.CacheDB
	ContextRef    context.ContextRef
	Notifications []*event.NotifyEventInfo
	Code          []byte
	Tx            *types.Transaction
	Time          uint32
	Height        uint32
	BlockHash     common.Uint256
	PreExec       bool
	GasPrice      uint64
	GasLimit      *uint64
	ExecStep      *uint64
	GasFactor     uint64
	IsTerminate   bool
	vm            *exec.VM
}

var (
	ERR_CHECK_STACK_SIZE  = errors.NewErr("[WasmVmService] vm over max stack size!")
	ERR_EXECUTE_CODE      = errors.NewErr("[WasmVmService] vm execute code invalid!")
	ERR_GAS_INSUFFICIENT  = errors.NewErr("[WasmVmService] gas insufficient")
	VM_EXEC_STEP_EXCEED   = errors.NewErr("[WasmVmService] vm execute step exceed!")
	CONTRACT_NOT_EXIST    = errors.NewErr("[WasmVmService] Get contract code from db fail")
	DEPLOYCODE_TYPE_ERROR = errors.NewErr("[WasmVmService] DeployCode type error!")
	VM_EXEC_FAULT         = errors.NewErr("[WasmVmService] vm execute state fault!")
	VM_INIT_FAULT         = errors.NewErr("[WasmVmService] vm init state fault!")

	CODE_CACHE_SIZE      = 100
	CONTRACT_METHOD_NAME = "invoke"

	//max memory size of wasm vm
	WASM_MEM_LIMITATION  uint64 = 10 * 1024 * 1024
	VM_STEP_LIMIT               = 40000000
	WASM_CALLSTACK_LIMIT        = 1024


	// todo 全局的 lru， 用于缓存 module 的
	CodeCache *lru.ARCCache
)

func init() {
	CodeCache, _ = lru.NewARC(CODE_CACHE_SIZE)
	//if err != nil{
	//	log.Info("NewARC block error %s", err)
	//}
}

/**
todo wasm合约的执行入口。

todo WASM 调 系统合约
	 WASM 调 NEO合约
	 WASM 调 WASM合约

todo 注意了，这些和NEO一样都做成 指令码， 只不过 是 wagon 的指令码
	所以在 wasm的 module中需要自己定义 对应的跨合约调用。
	在这里头, compiled, err = ReadWasmModule(wasmCode, false)
 */
func (this *WasmVmService) Invoke() (interface{}, error) {

	// 先校验 tx.Data 的长度
	if len(this.Code) == 0 {
		return nil, ERR_EXECUTE_CODE
	}


	// wasm 合约执行上下文
	contract := &states.WasmContractParam{}

	// 返回一个 `ZeroCopySource`, ZeroCopySource里头只有 `tx.Data []byte` 和 `off uint64` 当前reading index
	sink := common.NewZeroCopySource(this.Code)

	// 反序列化出 tx.Data中的 被调用`合约地址`和`本次调用的参数`
	err := contract.Deserialization(sink)
	if err != nil {
		return nil, err
	}

	// todo 根据合约的地址获取合约的code的封装  DeployCode
	code, err := this.CacheDB.GetContract(contract.Address)
	if err != nil {
		return nil, err
	}

	if code == nil {
		return nil, errors.NewErr("wasm contract does not exist")
	}

	// todo 这个才是真正的 code
	wasmCode, err := code.GetWasmCode()
	if err != nil {
		return nil, errors.NewErr("not a wasm contract")
	}

	// todo 将 SmartContract.Contexts 追加赋值 <Context是一个上下问而已，这里SmartContract.Contexts是多个哦>
	this.ContextRef.PushContext(&context.Context{ContractAddress: contract.Address, Code: wasmCode})

	// todo host 其实指的就是 runtime
	host := &Runtime{Service: this, Input: contract.Args}

	//wagon 里面的 CompileModule
	var compiled *exec.CompiledModule
	if CodeCache != nil {
		cached, ok := CodeCache.Get(contract.Address.ToHexString())
		if ok {
			compiled = cached.(*exec.CompiledModule)
		}
	}


	// todo 当获取不到的时候，需要新建 module
	if compiled == nil {

		// todo 根据 code 获取一个 module
		compiled, err = ReadWasmModule(wasmCode, false)
		if err != nil {
			return nil, err
		}

		// 追加到lru中
		CodeCache.Add(contract.Address.ToHexString(), compiled)
	}


	// todo 根据 module 创建一个 wagon-vm
	vm, err := exec.NewVMWithCompiled(compiled, WASM_MEM_LIMITATION)
	if err != nil {
		return nil, VM_INIT_FAULT
	}

	/**
	todo 给 vm 的各项赋值
	 */
	vm.HostData = host

	vm.AvaliableGas = &exec.Gas{GasLimit: this.GasLimit, LocalGasCounter: 0, GasPrice: this.GasPrice, GasFactor: this.GasFactor, ExecStep: this.ExecStep}
	vm.CallStackDepth = uint32(WASM_CALLSTACK_LIMIT)
	vm.RecoverPanic = true

	// todo 合约的调用入口函数, invoke
	entryName := CONTRACT_METHOD_NAME

	// todo 从 export 中根据 Name 导出对应的 条目 entry
	entry, ok := compiled.RawModule.Export.Entries[entryName]

	if ok == false {
		return nil, errors.NewErr("[Call]Method:" + entryName + " does not exist!")
	}

	//get entry index
	// todo entry的Id
	index := int64(entry.Index)

	//get function index
	// todo 根据 条目的Id 获取对应的 funcId
	fidx := compiled.RawModule.Function.Types[int(index)]

	//get  function type
	// todo 根据 function Id获取对应的 function类型
	ftype := compiled.RawModule.Types.Entries[int(fidx)]

	//no returns of the entry function
	// todo 根据function的类型判断是否存在 返回类型
	if len(ftype.ReturnTypes) > 0 {
		return nil, errors.NewErr("[Call]ExecCode error! Invoke function sig error")
	}

	//no args for passed in, all args in runtime input buffer
	this.vm = vm

	/**
	todo #####################################
	todo #####################################
	todo #####################################
	todo
	todo 执行指令码， 调用本次合约调用
	todo
	todo tx.Data 已经被引用到 `vm.HostData` 上了

	 */
	_, err = vm.ExecCode(index)

	if err != nil {
		return nil, errors.NewErr("[Call]ExecCode error!" + err.Error())
	}

	//pop the current context todo <执行完本次合约调用后，将 执行引擎中的上下文 移除掉>
	this.ContextRef.PopContext()

	// todo 注意： 本体没拿 执行的直接返回值，而是拿了 host.OutPut 中的返回值内容
	return host.Output, nil
}
