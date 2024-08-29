# sunshine
sunshine
安装sunshine之前先安装依赖go和protoc，如果安装过可以跳过这个步骤。

建议使用go 1.20以上版本： https://studygolang.com/dl

注：如果不能科学上网，获取github的库可能会遇到超时失败问题，建议设置为国内代理，执行命令 go env -w GOPROXY=https://goproxy.cn,direct


✅ 安装 protoc

下载protoc地址： https://github.com/protocolbuffers/protobuf/releases/tag/v25.2

根据系统类型下载对应的 protoc 可执行文件，把 protoc 可执行文件移动到与 go 可执行文件同一个目录。

安装 sunshine
安装完go和protoc之后，接下来安装sunshine及其插件，支持在windows、mac、linux和docker环境安装。

✅ 安装 git for windows

如果已经安装过，可以跳过安装git步骤。

下载git地址： [Git-2.44.0-64-bit.exe](https://github.com/git-for-windows/git/releases/download/v2.44.0.windows.1/Git-2.44.0-64-bit.exe)

下载后安装git，安装过程一直默认即可。安装git之后在任意文件夹下右键(显示更多选项)，如果有选择【Open Git Bash here】打开git bash终端，说明已经安装git成功。

Tip

解决git bash显示中文乱码，右键git bash终端，选择菜单【options】 --> 【Text】，找到character set，选择UTF-8后保存。


✅ 安装 make

下载mingw64地址： [x86_64-8.1.0-release-posix-seh-rt_v6-rev0.7z](https://sourceforge.net/projects/mingw-w64/files/Toolchains%20targetting%20Win64/Personal%20Builds/mingw-builds/8.1.0/threads-posix/seh/x86_64-8.1.0-release-posix-seh-rt_v6-rev0.7z)

解压文件，在bin目录下的找到mingw32-make.exe可执行文件，复制并改名为make.exe，把make.exe可执行文件移动到GOBIN目录(go env GOBIN查看，如果为空，下面有GOBIN设置说明)。

查看make版本：make -v

安装sunshine及其插件

打开git bash终端(不是windows自带的cmd)。

(1) 把GOBIN添加到系统环境变量path，如果已经设置过可以跳过此步骤。

# 设置 go get 命令下载第三方包的目录
setx GOPATH "D:\你的目录"
# 设置 go install 命令编译后生成可执行文件的存放目录
setx GOBIN "D:\你的目录\bin"

# 关闭当前终端，然后开启一个新的终端，查看GOBIN目录
go env GOBIN

(2) 把sunshine及其依赖插件安装到GOBIN目录下。

# 安装sunshine
go install github.com/18721889353/sunshine/cmd/sunshine@latest


# 初始化sunshine，自动安装sunshine依赖插件
sunshine init

# 查看插件是否都安装成功，如果发现有插件没有安装成功，执行命令重试 sunshine plugins --install
sunshine plugins

# 查看sunshine版本
sunshine -v


Tip
升级最新sunshine版本，执行命令 sunshine upgrade
