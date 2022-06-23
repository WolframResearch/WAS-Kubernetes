# HTTP over SSL Configuration Guide for WAS

## Introduction
This document describes how to add SSL support to Wolfram Application Server (WAS) using cert-manager within your Kubernetes cluster.

**Note:** This  is not a mandatory setup guide, but rather a reference guide for configuring SSL certificates for WAS if desired.

## Prerequisites

* A valid domain address for SSL (SSL certificates impose a 64 character limit on the common name which the default cluster domain name excceeds for some environments)
* A running WAS Cluster
* Admin Role access to the WAS Cluster

## Tools needed for SSL Certificate management
The following tools are required to create SSL certifcate for our WAS Cluster.

* [**kubectl**](https://kubernetes.io/docs/tasks/tools/install-kubectl/)

    **kubectl** controls the Kubernetes cluster manager.

* [**cert-manager**](https://cert-manager.io/docs/installation/)

    The **cert-manager** automates the provisioning of certificates within Kubernetes clusters. It provides a set of custom resources to issue certificates and attach them to services.

## Installation of a certificate

### Obtain a certificate

To enable HTTPS on your website, you need to obtain a certificate from a Certificate Authority (CA). [*Let's Encrypt*](https://letsencrypt.org/) is a free, automated, and open certificate authority (CA) provided by the [Internet Security Research Group (ISRG)](https://www.abetterinternet.org/). In this document it will be assumed that you will obtain your domain's certificate from *Let's Encrypt*. That being the case you must demonstrate control over the domain using software that supports the ACME protocol which typically runs on your web host.
 
### Install [cert-manager](https://cert-manager.io/docs/installation/)

To install **cert-manager** run

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.0/cert-manager.yaml
```

### Deploy ClusterIssuer

Edit the **spec.acme.email** field with the email address in the cluster-issuer.yaml file and deploy it to the cluster with

   ```bash
   kubectl apply -f cluster-issuer.yaml
   ```

### Deploy the Certificate

In the certificate.yaml file, update the **spec.dnsNames** field to 

```
spec:
  dnsNames:
    - <DOMAIN_WITHOUT_WWW>
```

and deploy it to the cluster with

```bash
kubectl apply -f certificate.yaml
```

### Configure Ingress

Within WAS there are five ingress objects which will require configuring:

* was-ingress-awes
* was-ingress-endpoints
* was-ingress-nodefiles
* was-ingress-resources
* was-ingress-endpoints-restart-rollout

You can list these ingress objects with `kubectl get ingress`.

To get the current configuration state for each object run

```bash
kubectl get ingress <INGRESS_NAME> -o yaml > <INGRESS_NAME>.yaml
```

which will export the current ingress file from the cluster. For each object, edit the following entries.

Add the **force-ssl-redirect** and **ssl-redirect** annotations set to "*false"* under **metadata.annotations**

```
...
nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
nginx.ingress.kubernetes.io/ssl-redirect: "false"
...
```

Add a **host** entry under **spec.rules** as

```
spec:
  ingressClassName: nginx
  rules:
    - host: <DOMAIN_WITHOUT_WWW>
      http:
        paths:
        - backend:
...
```

Finally, add a **tls** entry under **spec** as

```
spec:
	.
	.
	tls:
    - hosts:
        - <DOMAIN_WITHOUT_WWW>
      secretName: was-tls-secret
```

**warning:** Syntax errors in a configuration file may disable an object or cause erratic behavior. You can check your configuration file for mistakes by running

```bash
kubectl apply -f <INGRESS_NAME>.yaml --dry-run=server
```

This command will not make any changes on the cluster.

When the ingress object's configuration is satisfactorily updated, modify it on the cluster by running

```bash
kubectl apply -f <INGRESS_NAME>.yaml
```

The SSL certificate should be added in couple of minutes.


#### Sample ingress files

**was-ingress-awes.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/load-balance: ewma
    nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
  name: was-ingress-awes
  namespace: was
spec:
  ingressClassName: nginx
  rules:
    - host: aws.applicationserver.wolfram.com
      http:
        paths:
        - backend:
            service:
              name: active-web-elements-server
              port:
                number: 8080
          path: /
          pathType: Prefix
  tls:
      - hosts:
          - aws.applicationserver.wolfram.com
        secretName: was-tls-secret

```

**was-ingress-endpoints.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/rewrite-target: /endpoints/$1
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
  name: was-ingress-endpoints
  namespace: was
spec:
  ingressClassName: nginx
  rules:
    - host: aws.applicationserver.wolfram.com
      http:
        paths:
        - backend:
            service:
              name: endpoint-manager
              port:
                number: 8085
          path: /endpoints/?(.*)
          pathType: Prefix
  tls:
      - hosts:
          - aws.applicationserver.wolfram.com
        secretName: was-tls-secret

```

**was-ingress-nodefiles.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/rewrite-target: /nodefiles/$1
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
  name: was-ingress-nodefiles
  namespace: was
spec:
  ingressClassName: nginx
  rules:
    - host: aws.applicationserver.wolfram.com
      http:
        paths:
        - backend:
            service:
              name: resource-manager
              port:
                number: 9090
          path: /nodefiles/?(.*)
          pathType: Prefix
  tls:
      - hosts:
          - aws.applicationserver.wolfram.com
        secretName: was-tls-secret
```

**was-ingress-resources.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/rewrite-target: /resources/$1
    nginx.ingress.kubernetes.io/use-regex: "true"
    cert-manager.io/cluster-issuer: letsencrypt-cluster-issuer
    cert-manager.io/acme-challenge-type: "http01"
    nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
  name: was-ingress-resources
  namespace: was
spec:
  ingressClassName: nginx
  rules:
    - host: aws.applicationserver.wolfram.com
      http:
        paths:
        - backend:
            service:
              name: resource-manager
              port:
                number: 9090
          path: /resources/?(.*)
          pathType: Prefix
  tls:
      - hosts:
          - aws.applicationserver.wolfram.com
        secretName: was-tls-secret

```

**was-ingress-endpoints-restart-rollout.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/auth-realm: Authentication Required
    nginx.ingress.kubernetes.io/auth-secret: basic-auth
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/rewrite-target: /restart/kubernetes/active-web-elements-server-deployment
    nginx.ingress.kubernetes.io/use-regex: "true"
    nginx.ingress.kubernetes.io/force-ssl-redirect: "false"
    nginx.ingress.kubernetes.io/ssl-redirect: "false"
  labels:
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/part-of: ingress-nginx
  name: was-ingress-endpoints-restart-rollout
  namespace: was
spec:
  ingressClassName: nginx
  rules:
    - host: aws.applicationserver.wolfram.com
      http:
        paths:
        - backend:
            service:
              name: endpoint-manager
              port:
                number: 8085
          path: /.applicationserver/kernel/restart
          pathType: Prefix
  tls:
      - hosts:
          - aws.applicationserver.wolfram.com
        secretName: was-tls-secret
```

### Update AWES Deployment

If you have created a new domain as part of the certification installation process, you will need to update *active-web-elements-server-deployment* deployment file with the new domain. To do so run

```bash
kubectl edit deployment active-web-elements-server-deployment -n was
```

And add the domains to **spec.template.spec.containers.env** as

```
        - name: applicationserver.servername
          value: http://<DOMAIN>/
        - name: applicationserver.resourcemanager.url
          value: http://<DOMAIN>/resources/
        - name: applicationserver.nodefilesmanager.url
          value: http://<DOMAIN>/nodefiles/
        - name: applicationserver.endpointmanager.url
          value: http://<DOMAIN>/endpoints/
        - name: applicationserver.restart.url
          value: http://<DOMAIN>/.applicationserver/kernel/restart

```

The **edit** command uses the **vi** text editor as its default, but this is [configurable](https://docs.vmware.com/en/VMware-vSphere/7.0/vmware-vsphere-with-tanzu/GUID-DC2BB6E0-A327-4DB8-9A87-5F3376E70033.html#:~:text=To%20use%20the%20kubectl%20edit%20command%2C%20create%20a,knows%20when%20you%20have%20committed%20%28saved%29%20your%20changes.).

When you save the files, the *active-web-elements-server-deployment* pods will be restarted.

### HTTP Strict Transport Security (HSTS)

By default the Nginx ingress controller forces the browser to use TLS with the [`Strict-Transport-Security`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Strict-Transport-Security) header. You may wish to disable it to support the use of both HTTP and HTTPS with **ServiceConnect** Wolfram Languate client or other clients.

You will need to update then *ingress-nginx-controller* config map. Run

```bash
kubectl edit configmap ingress-nginx-controller -n ingress-nginx
```

and add **hsts: "False"** in the data section as

```
apiVersion: v1
data:
  client-max-body-size: 1G
  hsts: "False"
...
```

Now restart the **ingress-nginx-controller** deployment by running

```bash
kubectl rollout restart deployment ingress-nginx-controller -n ingress-nginx
```

The changes will take effect when the rolling restart has completed.

## Troubleshooting

* To check the **cert-manager**'s pod logs which are in the *cert-manager* namespace run

    ```bash
    kubectl logs cert-manager-<HASH> -n cert-manager
    ```

* To check the current challanges which the **cert-manager** working on run

    ```bash
    kubectl describe challenges
    ```

* To check the ingress objects events run

    ```bash
    kubectl describe ingress <INGRESS>
    ```

* To check for the presesence of a **was-tls-secret** run

    ```bash
    kubectl get secrets -n was
    ```

* To check for the preseseence of a **was-certificate** run

    ```bash
    kubectl describe certificate was-certificate -n was
    ```
    
* To check for a **letsencrypt-cluster-issuer** clusterissuer run

    ```bash
    kubectl describe clusterissuer letsencrypt-cluster-issuer
    ```

* To check the **nginx-controller** logs first find the **nginx-controller** pod by running

    ```bash
    kubectl get pods -n ingress-nginx
    ```
    
	The pod's naming typically follows the convention **ingress-nginx-controller-HASH**. Then run

	```bash
	kubectl logs <POD_NAME>
	```

	to read the log.