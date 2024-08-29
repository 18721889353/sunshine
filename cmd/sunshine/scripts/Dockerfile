FROM alpine:latest
MAINTAINER 18721889353 "g.18721889353@gmail.com"

USER root

# set the time zone to Shanghai
#RUN apk add tzdata  \
#    && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
#    && echo "Asia/Shanghai" > /etc/timezone \
#    && apk del tzdata

COPY .sunshine /root/.sunshine
COPY sunshine /usr/local/bin/sunshine
RUN chmod +x /usr/local/bin/sunshine

EXPOSE 24631

WORKDIR /app

ENTRYPOINT ["sunshine", "run"]
