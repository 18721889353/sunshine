FROM jenkins/jenkins:2.370
LABEL maintainer="18721889353 g.18721889353@gmail.com"
USER root

RUN apt-get update -y && apt-get dist-upgrade -y && apt-get install -y apt-utils make
RUN ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo "Asia/Shanghai" > /etc/timezone

RUN curl -L -o go1.19.tar.gz https://studygolang.com/dl/golang/go1.19.1.linux-amd64.tar.gz \
    && tar zxf go1.19.tar.gz \
    && rm -f go1.19.tar.gz \
    && mv -f go /opt/

RUN echo 'export GOROOT="/opt/go"' >> ~/.bashrc \
    && echo 'export GOPATH=$HOME/go' >> ~/.bashrc \
    && echo 'export GOBIN=$GOROOT/bin' >> ~/.bashrc \
    && echo 'export PATH=$PATH:$GOBIN' >> ~/.bashrc
