#!/bin/bash

protoBasePath="api"
allProtoFiles=""

specifiedProtoFilePath=$1
specifiedProtoFilePaths=""

colorGray='\033[1;30m'
colorGreen='\033[1;32m'
colorMagenta='\033[1;35m'
colorCyan='\033[1;36m'
highBright='\033[1m'
markEnd='\033[0m'

tipMsg=""

function checkResult() {
    result=$1
    if [ ${result} -ne 0 ]; then
        exit ${result}
    fi
}

# 获取指定的 proto 文件，为空返回 0，否则返回 1
function getSpecifiedProtoFiles() {
  if [ "$specifiedProtoFilePath"x = x ];then
    return 0
  fi

  specifiedProtoFilePaths=${specifiedProtoFilePath//,/ }

  for v in $specifiedProtoFilePaths; do
    if [ ! -f "$v" ];then
      echo "Error: not found specified proto file $v"
	    echo "example: make proto FILES=api/user/v1/user.proto,api/types/types.proto"
      checkResult 1
    fi
  done

  return 1
}

# 在此删除生成的 *.pb.go 代码中无用包的 import
function deleteUnusedPkg() {
  file=$1
  osType=$(uname -s)
  if [ "${osType}"x = "Darwin"x ];then
    sed -i '' 's#_ \"github.com/envoyproxy/protoc-gen-validate/validate\"##g' ${file}
    sed -i '' 's#_ \"github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2/options\"##g' ${file}
    sed -i '' 's#_ \"github.com/srikrsna/protoc-gen-gotag/tagger\"##g' ${file}
    sed -i '' 's#_ \"google.golang.org/genproto/googleapis/api/annotations\"##g' ${file}
  else
    sed -i "s#_ \"github.com/envoyproxy/protoc-gen-validate/validate\"##g" ${file}
    sed -i "s#_ \"github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2/options\"##g" ${file}
    sed -i "s#_ \"github.com/srikrsna/protoc-gen-gotag/tagger\"##g" ${file}
    sed -i "s#_ \"google.golang.org/genproto/googleapis/api/annotations\"##g" ${file}
  fi
  checkResult $?
}

function listProtoFiles(){
    cd $1
    items=$(ls)

    for item in $items; do
        if [ -d "$item" ]; then
            listProtoFiles $item
        else
            if [ "${item#*.}"x = "proto"x ];then
              file=$(pwd)/${item}
              protoFile="${protoBasePath}${file#*${protoBasePath}}"
              allProtoFiles="${allProtoFiles} ${protoFile}"
            fi
        fi
    done
    cd ..
}

function handlePbGoFiles(){
    cd $1
    items=$(ls)

    for item in $items; do
        if [ -d "$item" ]; then
            handlePbGoFiles $item
        else
            if [ "${item#*.}"x = "pb.go"x ];then
              deleteUnusedPkg $item
            fi
        fi
    done
    cd ..
}

function patchTypesPbFile() {
  for file in $allProtoFiles; do
    if [  "$file" = "api/types/types.proto"  ]; then
      return
    fi
    if grep -q "api/types/types.proto" "$file"; then
      allProtoFiles=$allProtoFiles" api/types/types.proto"
      bash scripts/patch.sh types-pb
      return
    fi
  done
}

function getModuleName() {
  if [ -f "go.mod" ]; then
    head -1 go.mod | awk '{print $2}'
  fi
}

function autoDetectInitDbFile() {
  moduleName=$(getModuleName)
  if [ -z "$moduleName" ]; then
    echo "Warning: go.mod not found, skip gen-db-init"
    return
  fi
  sunshine patch gen-db-init --module-name="$moduleName" --out=. > /dev/null
}

function generateByAllProto(){
  getSpecifiedProtoFiles
  if [ $? -eq 0 ]; then
    listProtoFiles $protoBasePath
  else
    allProtoFiles=$specifiedProtoFilePaths
  fi

  patchTypesPbFile
  autoDetectInitDbFile

  if [ "$allProtoFiles"x = x ];then
    echo "Error: not found proto file in path $protoBasePath"
    exit 1
  fi
  echo -e "generate *pb.go by proto files: ${colorGray}$allProtoFiles${markEnd}"
  echo ""

  # 生成 *_pb.go 文件
  protoc --proto_path=. --proto_path=./third_party \
    --go_out=. --go_opt=paths=source_relative \
    $allProtoFiles

  checkResult $?
  # todo generate grpc files here
  # delete the templates code start
  # 生成 *_grpc_pb.go 文件
  protoc --proto_path=. --proto_path=./third_party \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    $allProtoFiles

  checkResult $?
  # delete the templates code end

  # 生成 *_pb.validate.go 文件
  protoc --proto_path=. --proto_path=./third_party \
    --validate_out=lang=go:. --validate_opt=paths=source_relative \
    $allProtoFiles

  checkResult $?

  # 将 tag 字段嵌入到 *_pb.go 中
  protoc --proto_path=. --proto_path=./third_party \
    --gotag_out=:. --gotag_opt=paths=source_relative \
    $allProtoFiles

  checkResult $?
}

function generateBySpecifiedProto(){
  # 获取 serverNameExample 服务的 proto 文件
  allProtoFiles=""
  listProtoFiles ${protoBasePath}/serverNameExample
  cd ..
  specifiedProtoFiles=""
  getSpecifiedProtoFiles
  if [ $? -eq 0 ]; then
    specifiedProtoFiles=$allProtoFiles
  else
	  for v1 in $specifiedProtoFilePaths; do
      for v2 in $allProtoFiles; do
        if [ "$v1"x = "$v2"x ];then
          specifiedProtoFiles="$specifiedProtoFiles $v1"
        fi
      done
	  done
  fi

  if [ "$specifiedProtoFiles"x = x ];then
    return
  fi
  echo -e "generate template code by proto files: ${colorMagenta}$specifiedProtoFiles${markEnd}"
  echo ""
  # todo generate api template code command here
  # delete the templates code start

  # 生成 swagger 文档，并将所有文件合并到 docs/apis.swagger.json
  protoc --proto_path=. --proto_path=./third_party \
    --openapiv2_out=. --openapiv2_opt=logtostderr=true --openapiv2_opt=allow_merge=true --openapiv2_opt=merge_file_name=docs/apis.json \
    $specifiedProtoFiles

  checkResult $?

  # 将 64 位字段类型从 string 转换为 integer
  sunshine web swagger --file=docs/apis.swagger.json > /dev/null
  checkResult $?

  # 共生成四个文件：注册路由文件 *_router.pb.go（与 protobuf 文件保存在同一目录）、
  # 注入路由文件 *_router.go（默认保存在 internal/routers）、逻辑代码模板文件 *.go（默认保存在 internal/service）、
  # 返回错误码模板文件 *_http.go（默认保存在 internal/ecode）
  protoc --proto_path=. --proto_path=./third_party \
    --go-gin_out=. --go-gin_opt=paths=source_relative --go-gin_opt=plugin=service \
    --go-gin_opt=moduleName=github.com/18721889353/sunshine --go-gin_opt=serverName=serverNameExample \
    $specifiedProtoFiles

  sunshine merge rpc-gw-pb
  checkResult $?

  tipMsg="${highBright}Tip:${markEnd} execute the command ${colorCyan}make run${markEnd} and then visit ${colorCyan}http://localhost:8080/apis/swagger/index.html${markEnd} in your browser."
  # delete the templates code end

  if [ "$suitedMonoRepo" == "true" ]; then
    sunshine patch adapt-mono-repo --dir=serverNameExample
  fi
}

# 根据所有 proto 文件生成 pb.go
generateByAllProto

# 根据指定的 proto 文件生成 pb.go
generateBySpecifiedProto

# 删除 pb.go 中未使用的包
handlePbGoFiles $protoBasePath

# 删除 json tag 中的 omitempty
sunshine patch del-omitempty --dir=$protoBasePath --suffix-name=pb.go > /dev/null

# 修改重复的错误码编号
sunshine patch modify-dup-num --dir=internal/ecode
sunshine patch modify-dup-err-code --dir=internal/ecode
moduleName=$(getModuleName)
sunshine patch gen-db-init --db-driver=mysql --module-name="$moduleName" --out=./
sunshine patch gen-types-pb --module-name="$moduleName" --out=./
sunshine config --server-dir=.
echo -e "${colorGreen}generated code done.${markEnd}"
echo ""
echo -e $tipMsg
echo ""
