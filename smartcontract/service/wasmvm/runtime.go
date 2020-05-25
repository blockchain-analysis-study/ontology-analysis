/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,0x0000000000000000000000000000000000000003
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */
package wasmvm

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/go-interpreter/wagon/exec"
	"github.com/go-interpreter/wagon/wasm"
	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/core/payload"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/errors"
	"github.com/ontio/ontology/smartcontract/event"
	native2 "github.com/ontio/ontology/smartcontract/service/native"
	"github.com/ontio/ontology/smartcontract/service/native/utils"
	"github.com/ontio/ontology/smartcontract/service/util"
	"github.com/ontio/ontology/smartcontract/states"
	"github.com/ontio/ontology/vm/crossvm_codec"
	neotypes "github.com/ontio/ontology/vm/neovm/types"
	"io"
)

type ContractType byte

const (
	NATIVE_CONTRACT ContractType = iota
	NEOVM_CONTRACT
	WASMVM_CONTRACT
	UNKOWN_CONTRACT
)

type Runtime struct {
	Service    *WasmVmService
	Input      []byte
	Output     []byte
	CallOutPut []byte
}

func TimeStamp(proc *exec.Process) uint64 {
	self := proc.HostData().(*Runtime)
	self.checkGas(TIME_STAMP_GAS)
	return uint64(self.Service.Time)
}

func BlockHeight(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(BLOCK_HEGHT_GAS)
	return self.Service.Height
}

func SelfAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(SELF_ADDRESS_GAS)
	selfaddr := self.Service.ContextRef.CurrentContext().ContractAddress
	_, err := proc.WriteAt(selfaddr[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func Sha256(proc *exec.Process, src uint32, slen uint32, dst uint32) {
	self := proc.HostData().(*Runtime)
	cost := uint64((slen/1024)+1) * SHA256_GAS
	self.checkGas(cost)

	bs, err := ReadWasmMemory(proc, src, slen)
	if err != nil {
		panic(err)
	}

	sh := sha256.New()
	sh.Write(bs[:])
	hash := sh.Sum(nil)

	_, err = proc.WriteAt(hash[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func CallerAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(CALLER_ADDRESS_GAS)
	if self.Service.ContextRef.CallingContext() != nil {
		calleraddr := self.Service.ContextRef.CallingContext().ContractAddress
		_, err := proc.WriteAt(calleraddr[:], int64(dst))
		if err != nil {
			panic(err)
		}
	} else {
		_, err := proc.WriteAt(common.ADDRESS_EMPTY[:], int64(dst))
		if err != nil {
			panic(err)
		}
	}

}

func EntryAddress(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	self.checkGas(ENTRY_ADDRESS_GAS)
	entryAddress := self.Service.ContextRef.EntryContext().ContractAddress
	_, err := proc.WriteAt(entryAddress[:], int64(dst))
	if err != nil {
		panic(err)
	}
}

func Checkwitness(proc *exec.Process, dst uint32) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(CHECKWITNESS_GAS)
	var addr common.Address
	_, err := proc.ReadAt(addr[:], int64(dst))
	if err != nil {
		panic(err)
	}

	address, err := common.AddressParseFromBytes(addr[:])
	if err != nil {
		panic(err)
	}

	if self.Service.ContextRef.CheckWitness(address) {
		return 1
	}
	return 0
}

func Ret(proc *exec.Process, ptr uint32, len uint32) {
	self := proc.HostData().(*Runtime)
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		panic(err)
	}

	self.Output = bs
	proc.Terminate()
}

func Debug(proc *exec.Process, ptr uint32, len uint32) {
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		//do not panic on debug
		return
	}

	log.Debugf("[WasmContract]Debug:%s\n", bs)
}

func Notify(proc *exec.Process, ptr uint32, l uint32) {
	self := proc.HostData().(*Runtime)
	if l >= neotypes.MAX_NOTIFY_LENGTH {
		panic("notify length over the uplimit")
	}
	bs, err := ReadWasmMemory(proc, ptr, l)
	if err != nil {
		panic(err)
	}
	notify := &event.NotifyEventInfo{ContractAddress: self.Service.ContextRef.CurrentContext().ContractAddress}
	val := crossvm_codec.DeserializeNotify(bs)
	notify.States = val

	notifys := make([]*event.NotifyEventInfo, 1)
	notifys[0] = notify
	self.Service.ContextRef.PushNotifications(notifys)
}

func InputLength(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	return uint32(len(self.Input))
}

// todo 获取 合约函数调用入参
func GetInput(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	_, err := proc.WriteAt(self.Input, int64(dst))
	if err != nil {
		panic(err)
	}
}

func CallOutputLength(proc *exec.Process) uint32 {
	self := proc.HostData().(*Runtime)
	return uint32(len(self.CallOutPut))
}

func GetCallOut(proc *exec.Process, dst uint32) {
	self := proc.HostData().(*Runtime)
	_, err := proc.WriteAt(self.CallOutPut, int64(dst))
	if err != nil {
		panic(err)
	}
}

func GetCurrentTxHash(proc *exec.Process, ptr uint32) uint32 {
	self := proc.HostData().(*Runtime)
	self.checkGas(CURRENT_TX_HASH_GAS)

	txhash := self.Service.Tx.Hash()

	length, err := proc.WriteAt(txhash[:], int64(ptr))
	if err != nil {
		panic(err)
	}

	return uint32(length)
}

func RaiseException(proc *exec.Process, ptr uint32, len uint32) {
	bs, err := ReadWasmMemory(proc, ptr, len)
	if err != nil {
		//do not panic on debug
		return
	}

	panic(fmt.Errorf("[RaiseException]Contract RaiseException:%s\n", bs))
}

/**
TODO  WASM 合约调用合约
 */
func CallContract(proc *exec.Process, contractAddr uint32, inputPtr uint32, inputLen uint32) uint32 {
	self := proc.HostData().(*Runtime)

	self.checkGas(CALL_CONTRACT_GAS)
	var contractAddress common.Address
	_, err := proc.ReadAt(contractAddress[:], int64(contractAddr))
	if err != nil {
		panic(err)
	}

	inputs, err := ReadWasmMemory(proc, inputPtr, inputLen)
	if err != nil {
		panic(err)
	}

	// todo 根据被调用的合约，检出合约的类型
	contracttype, err := self.getContractType(contractAddress)
	if err != nil {
		panic(err)
	}

	var result []byte


	/**
	todo 根据被调用合约的类型处理各种合约调用
	 */
	switch contracttype {

	// 系统合约调用
	case NATIVE_CONTRACT:
		// todo 根据 tx.Data 解出
		source := common.NewZeroCopySource(inputs)

		// todo 从 input 中取出 本次需要调用合约的入参版本
		ver, eof := source.NextByte()
		if eof {
			panic(io.ErrUnexpectedEOF)
		}
		method, _, irregular, eof := source.NextString()
		if irregular {
			panic(common.ErrIrregularData)
		}
		if eof {
			panic(io.ErrUnexpectedEOF)
		}

		args, _, irregular, eof := source.NextVarBytes()
		if irregular {
			panic(common.ErrIrregularData)
		}
		if eof {
			panic(io.ErrUnexpectedEOF)
		}

		// todo 构建合约上下文
		contract := states.ContractInvokeParam{
			Version: ver,
			Address: contractAddress,
			Method:  method,
			Args:    args,
		}

		self.checkGas(NATIVE_INVOKE_GAS)
		native := &native2.NativeService{
			CacheDB:     self.Service.CacheDB,
			InvokeParam: contract,
			Tx:          self.Service.Tx,
			Height:      self.Service.Height,
			Time:        self.Service.Time,
			ContextRef:  self.Service.ContextRef,
			ServiceMap:  make(map[string]native2.Handler),
			PreExec:     self.Service.PreExec,
		}

		/**
		todo 调用 系统合约
		 */
		tmpRes, err := native.Invoke()
		if err != nil {
			panic(errors.NewErr("[nativeInvoke]AppCall failed:" + err.Error()))
		}

		result = tmpRes

	// WASM 合约调用
	case WASMVM_CONTRACT:
		conParam := states.WasmContractParam{Address: contractAddress, Args: inputs}
		param := common.SerializeToBytes(&conParam)

		/**
		todo 新实例化一个  WASM 合约执行引擎
		 */
		newservice, err := self.Service.ContextRef.NewExecuteEngine(param, types.InvokeWasm)
		if err != nil {
			panic(err)
		}

		// todo 调用 WASM合约
		tmpRes, err := newservice.Invoke()
		if err != nil {
			panic(err)
		}

		result = tmpRes.([]byte)

	// NEO 合约调用
	case NEOVM_CONTRACT:
		evalstack, err := util.GenerateNeoVMParamEvalStack(inputs)
		if err != nil {
			panic(err)
		}

		// todo 如果是 WASM -> NEO 跨虚机调用， 则需要 新起一个 NEO 的引擎
		neoservice, err := self.Service.ContextRef.NewExecuteEngine([]byte{}, types.InvokeNeo)
		if err != nil {
			panic(err)
		}

		err = util.SetNeoServiceParamAndEngine(contractAddress, neoservice, evalstack)
		if err != nil {
			panic(err)
		}

		// todo 调用 NEO合约
		tmp, err := neoservice.Invoke()
		if err != nil {
			panic(err)
		}

		if tmp != nil {
			val := tmp.(*neotypes.VmValue)
			source := common.NewZeroCopySink([]byte{byte(crossvm_codec.VERSION)})

			err = neotypes.BuildResultFromNeo(*val, source)
			if err != nil {
				panic(err)
			}
			result = source.Bytes()
		}

	default:
		panic(errors.NewErr("Not a supported contract type"))
	}

	self.CallOutPut = result
	return uint32(len(self.CallOutPut))
}

/**
创建一个 module
 */
func NewHostModule() *wasm.Module {
	m := wasm.NewModule()
	paramTypes := make([]wasm.ValueType, 14)
	for i := 0; i < len(paramTypes); i++ {
		paramTypes[i] = wasm.ValueTypeI32
	}


	// todo 构建各种类型 的 section
	m.Types = &wasm.SectionTypes{
		Entries: []wasm.FunctionSig{
			//func()uint64    [0]
			{
				Form:        0, // value for the 'func' type constructor
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI64},
			},
			//func()uint32     [1]
			{
				Form:        0, // value for the 'func' type constructor
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32)     [2]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32)uint32  [3]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32)  [4]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32)uint32  [5]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32,uint32,uint32)uint32  [6]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32,uint32,uint32,uint32)  [7]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
			//func(uint32,uint32)uint32   [8]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32},
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//func(uint32 * 14)uint32   [9]
			{
				Form:        0, // value for the 'func' type constructor
				ParamTypes:  paramTypes,
				ReturnTypes: []wasm.ValueType{wasm.ValueTypeI32},
			},
			//funct()   [10]
			{
				Form: 0, // value for the 'func' type constructor
			},
			//func(uint32,uint32,uint32)  [11]
			{
				Form:       0, // value for the 'func' type constructor
				ParamTypes: []wasm.ValueType{wasm.ValueTypeI32, wasm.ValueTypeI32, wasm.ValueTypeI32},
			},
		},
	}

	// 获得当前的时间戳，即返回调用该函数的 Unix 时间，单位为秒
	// todo ####################################
	// todo ####################################
	// todo ####################################
	//
	// todo 构建 各种 function的 section
	m.FunctionIndexSpace = []wasm.Function{
		{ //0
			Sig:  &m.Types.Entries[0],
			Host: reflect.ValueOf(TimeStamp), // todo 获取 timeStamp() 的外部函数； 获得当前的时间戳，即返回调用该函数的 Unix 时间，单位为秒
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //1
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(BlockHeight), // todo 获取 BlockNum() 的外部函数； 获得当前区块链网络的区块高度
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //2
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(InputLength), // todo 获取 tx.Data的长度 的外部函数
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //3
			Sig:  &m.Types.Entries[1],
			Host: reflect.ValueOf(CallOutputLength), // todo 获取 tx返回值长度 的外部函数
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //4
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(SelfAddress), // todo  获得当前合约的地址
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //5
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(CallerAddress), // todo 获得调用方的合约地址，主要用于跨合约调用的场景，比如合约 A 调用合约 B 的应用场景, 在合约 B 中就可以调用该方法获得调用方合约 A 的地址
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //6
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(EntryAddress), // todo 获得入口合约地址，比如有这样的应用场景，合约 A 通过合约 B 调用合约 C的方法，此时，在合约 C 中就可以通过该方法拿到合约 A 的地址
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //7
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(GetInput), // todo 获取 调用 input <跨合约时用到>
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //8
			Sig:  &m.Types.Entries[2],
			Host: reflect.ValueOf(GetCallOut), // todo 获取调用 output <跨合约时用到>
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //9
			Sig:  &m.Types.Entries[3],

			/**
			todo 校验是否含有该地址的签名
			CheckWitness(fromAcct) 有两个功能：

			验证当前的函数调用者是不是 fromAcct ,若是（验证签名），则验证通过。

			检查当前函数调用者是不是一个合约 A，若是合约 A，且是从合约 A 发起的去执行函数，则验证通过(验证 fromAcct 是不是GetCallingScriptHash() 的返回值)。
			 */
			/**
			https://dev-docs.ont.io/#/docs-cn/smartcontract/05-sc-faq
			6. 怎样能像 Ethereum 合约那样在合约内部拿到 msg.sender 及 msg.value 这种值？
			调用帐户在主动调用合约函数时，需要把自已的帐户地址及转账资产的大小以参数的形式传入合约。
			在合约内部使用 CheckWitness 对调用帐户进行验签，及进行资产额度的合法性判断。
			 */
			Host: reflect.ValueOf(Checkwitness),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //10
			Sig:  &m.Types.Entries[3],
			Host: reflect.ValueOf(GetCurrentBlockHash),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //11
			Sig:  &m.Types.Entries[3],
			Host: reflect.ValueOf(GetCurrentTxHash),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //12
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Ret),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //13
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Notify), // todo 将合约中事件推送到全网，并将其内容保存到链上
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //14
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(Debug), // todo 打印信息来调试被调用的合约函数，观察合约函数执行到哪一步出了问题
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //15
			Sig:  &m.Types.Entries[5],

			// todo ##############################
			// todo ##############################
			// todo ##############################
			// todo
			// todo 这个也是合约调合约的指令
			Host: reflect.ValueOf(CallContract), // todo  WASM 合约调用合约
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //16
			Sig:  &m.Types.Entries[6],
			Host: reflect.ValueOf(StorageRead),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //17
			Sig:  &m.Types.Entries[7],
			Host: reflect.ValueOf(StorageWrite),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //18
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(StorageDelete),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //19 todo 合约创建
			Sig:  &m.Types.Entries[9],
			Host: reflect.ValueOf(ContractCreate),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //20
			Sig:  &m.Types.Entries[9],

			// todo 合约迁移的功能的
			//
			// 其结果表现为旧合约失效，新合约生效，但旧合约的数据会全部被迁移到新合约中。
			// 注意调用该函数的手续费会受到新合约规模大小以及旧合约里的状态数据量有关。
			Host: reflect.ValueOf(ContractMigrate),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //21
			Sig:  &m.Types.Entries[10],
			Host: reflect.ValueOf(ContractDestroy),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //22
			Sig:  &m.Types.Entries[4],
			Host: reflect.ValueOf(RaiseException),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
		{ //23
			Sig:  &m.Types.Entries[11],
			Host: reflect.ValueOf(Sha256),
			Body: &wasm.FunctionBody{}, // create a dummy wasm body (the actual value will be taken from Host.)
		},
	}

	// todo 构造 module的 导出
	//
	// 这些和 m.FunctionIndexSpace 中的索引一一对应
	m.Export = &wasm.SectionExports{
		Entries: map[string]wasm.ExportEntry{
			"ontio_timestamp": {
				FieldStr: "ontio_timestamp",
				Kind:     wasm.ExternalFunction,
				Index:    0,
			},
			"ontio_block_height": {
				FieldStr: "ontio_block_height",
				Kind:     wasm.ExternalFunction,
				Index:    1,
			},
			"ontio_input_length": {
				FieldStr: "ontio_input_length",
				Kind:     wasm.ExternalFunction,
				Index:    2,
			},
			"ontio_call_output_length": {
				FieldStr: "ontio_call_output_length",
				Kind:     wasm.ExternalFunction,
				Index:    3,
			},
			"ontio_self_address": {
				FieldStr: "ontio_self_address",
				Kind:     wasm.ExternalFunction,
				Index:    4,
			},
			"ontio_caller_address": {
				FieldStr: "ontio_caller_address",
				Kind:     wasm.ExternalFunction,
				Index:    5,
			},
			"ontio_entry_address": {
				FieldStr: "ontio_entry_address",
				Kind:     wasm.ExternalFunction,
				Index:    6,
			},
			"ontio_get_input": {
				FieldStr: "ontio_get_input",
				Kind:     wasm.ExternalFunction,
				Index:    7,
			},
			"ontio_get_call_output": {
				FieldStr: "ontio_get_call_output",
				Kind:     wasm.ExternalFunction,
				Index:    8,
			},
			"ontio_check_witness": {
				FieldStr: "ontio_check_witness",
				Kind:     wasm.ExternalFunction,
				Index:    9,
			},
			"ontio_current_blockhash": {
				FieldStr: "ontio_current_blockhash",
				Kind:     wasm.ExternalFunction,
				Index:    10,
			},
			"ontio_current_txhash": {
				FieldStr: "ontio_current_txhash",
				Kind:     wasm.ExternalFunction,
				Index:    11,
			},
			"ontio_return": {
				FieldStr: "ontio_return",
				Kind:     wasm.ExternalFunction,
				Index:    12,
			},
			"ontio_notify": {
				FieldStr: "ontio_notify",
				Kind:     wasm.ExternalFunction,
				Index:    13,
			},
			"ontio_debug": {
				FieldStr: "ontio_debug",
				Kind:     wasm.ExternalFunction,
				Index:    14,
			},
			"ontio_call_contract": {
				FieldStr: "ontio_call_contract",
				Kind:     wasm.ExternalFunction,
				Index:    15,
			},
			"ontio_storage_read": {
				FieldStr: "ontio_storage_read",
				Kind:     wasm.ExternalFunction,
				Index:    16,
			},
			"ontio_storage_write": {
				FieldStr: "ontio_storage_write",
				Kind:     wasm.ExternalFunction,
				Index:    17,
			},
			"ontio_storage_delete": {
				FieldStr: "ontio_storage_delete",
				Kind:     wasm.ExternalFunction,
				Index:    18,
			},
			"ontio_contract_create": {
				FieldStr: "ontio_contract_create",
				Kind:     wasm.ExternalFunction,
				Index:    19,
			},
			"ontio_contract_migrate": {
				FieldStr: "ontio_contract_migrate",
				Kind:     wasm.ExternalFunction,
				Index:    20,
			},
			"ontio_contract_destroy": {
				FieldStr: "ontio_contract_destroy",
				Kind:     wasm.ExternalFunction,
				Index:    21,
			},
			"ontio_panic": {
				FieldStr: "ontio_panic",
				Kind:     wasm.ExternalFunction,
				Index:    22,
			},
			"ontio_sha256": {
				FieldStr: "ontio_sha256",
				Kind:     wasm.ExternalFunction,
				Index:    23,
			},
		},
	}

	return m
}

func (self *Runtime) getContractType(addr common.Address) (ContractType, error) {

	// todo 本地合约类型
	if utils.IsNativeContract(addr) {
		return NATIVE_CONTRACT, nil
	}

	dep, err := self.Service.CacheDB.GetContract(addr)
	if err != nil {
		return UNKOWN_CONTRACT, err
	}
	if dep == nil {
		return UNKOWN_CONTRACT, errors.NewErr("contract is not exist.")
	}

	// todo WASM 合约类型
	if dep.VmType() == payload.WASMVM_TYPE {
		return WASMVM_CONTRACT, nil
	}

	// todo 默认返回 NEO 合约类型
	return NEOVM_CONTRACT, nil

}

func (self *Runtime) checkGas(gaslimit uint64) {
	gas := self.Service.vm.AvaliableGas
	if *gas.GasLimit >= gaslimit {
		*gas.GasLimit -= gaslimit
	} else {
		panic(errors.NewErr("[wasm_Service]Insufficient gas limit"))
	}
}

func serializeStorageKey(contractAddress common.Address, key []byte) []byte {
	bf := new(bytes.Buffer)

	bf.Write(contractAddress[:])
	bf.Write(key)

	return bf.Bytes()
}
