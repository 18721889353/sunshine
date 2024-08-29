## Build and Push Image

```bash
# build sunshine image
./build-sunshine-image.sh v1.8.3

# copy tag
docker tag 18721889353/sunshine:v1.8.3 18721889353/sunshine:latest

# login docker
docker login -u 18721889353 -p

# push image
docker push 18721889353/sunshine:v1.8.3
docker push 18721889353/sunshine:latest
```