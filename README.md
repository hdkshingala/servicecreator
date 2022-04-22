# Service Creator Kubernetes Controller

It is a kubernetes controller which creates services in Kubernetes cluster if it receives a Deployment having annotation like `infracloud.io/service=PORT_VALUE`.

## How to run

- Run below command to generate Docker image:
    ```
    docker build -t <DOCKER_USERNAME>/servicecreator:1.0.0 .
    ```

- Push the generated image to Docker hub:
    ```
    docker push <DOCKER_USERNAME>/servicecreator:1.0.0
    ```

- Replace the image name in `manifest/deploy.yaml`.

- Run below command to deploy the controller on the Kubernetes:
    ```
    kubectl apply -f manifest/
    ```
