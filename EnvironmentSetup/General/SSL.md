## HTTP over SSL Configuration Guide for WAS
### Introduction
There are different ways to add Secure Sockets Layer(SSL) certificate for your application. This document describes how to add SSL support for Wolfram Application Server(WAS) using Kubernetes cert-manager. 

**Note:** This  is not a mandatory setup guide. This is only a reference guide to configure SSL certificate for WAS.
### Pre-Requisite

* A valid domain address for SSL
  * SSL certificate has 64character limit, so we can't use default cluster domain(it has more than ~70 characters)
* A running WAS Cluster
* Admin Role access to the WAS Cluster
* Needed the Kubernetes command-line tool, [kubect](https://kubernetes.io/docs/tasks/tools/)


### Tools needed for SSL Certificate
We need the following tools to create SSL certifcate for our WAS Cluster.
- [cert-manager](https://cert-manager.io/docs/installation/)
  - Cert-Manager automates the provisioning of certificates within Kubernetes clusters. It provides a set of custom resources to issue certificates and attach them to services.
- [Let's Encrypt](https://letsencrypt.org/)
  - To enable HTTPS on your website, you need to get a certificate (a type of file) from a Certificate Authority (CA). Let’s Encrypt is a CA.In order to get a certificate for your website’s domain from Let’s Encrypt, you have to demonstrate control over the domain. With Let’s Encrypt, you do this using software that uses the ACME protocol which typically runs on your web host.
  
---
### Install [cert-manager](https://cert-manager.io/docs/installation/)

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.8.0/cert-manager.yaml
```
It's quite straightforward. The version may change over time.

---


### Deploy ClusterIssuer

   Edit spec.acme.email field with the email address in cluster-issuer.yaml file and deploy it to k8s with

   ```bash
   kubectl apply -f cluster-issuer.yaml
   ```

---

### Deploy Certificate

In certificate.yaml file, update **spec.dnsNames** field as 

```
spec:
  dnsNames:
    - <DOMAIN_WITHOUT_WWW>
```

and deploy it to k8s with
```bash
kubectl apply -f certificate.yaml
```

---

### Configure Ingress

There are 5 ingress objects as 

* was-ingress-awes
* was-ingress-endpoints
* was-ingress-nodefiles
* was-ingress-resources
* was-ingress-endpoints-restart-rollout

You can list the ingress objects with `kubectl get ingress`

You need to get the current ingress objects configuration with

`kubectl get ingress <INGRESS_NAME> -o yaml > <INGRESS_NAME>.yaml`

It will export the current ingress file from the cluster.

So you can edit them as below and use `kubectl apply -f <INGRESS_NAME>.yaml` for apply changes.

Add **host** under **spec.rules** as

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

In ingress files, add **tls** under **spec** as

```
spec:
	.
	.
	tls:
    - hosts:
        - <DOMAIN_WITHOUT_WWW>
      secretName: was-tls-secret
```

**ps: Indentation is very important when updating YAML files. You can check the YAML file errors with**

**`kubectl apply -f <INGRESS_NAME>.yaml --dry-run=server`**

**This command doesn't make change on the cluster, only checks for errors.**

---

These are the sample ingress files

**was-ingress-awes.yaml**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/load-balance: ewma
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

Run `kubectl apply -f <INGRESS_NAME>.yaml` command to update ingress files.


SSL certificate should be added in couple of minutes.

---

## Troubleshooting

   * Check cert-manager's pod logs which is in cert-manager namespaces.


   	kubectl logs cert-manager-<HASH> -n cert-manager

* Run `kubectl describe challanges` to check current challanges which is cert-manager working on.
* Check ingress objects events with `kubectl describe ingress <INGRESS>`
* Check for `was-tls-secret` secret with `kubectl get secrets -n was`
* Check for `was-certificate` certificate with `kubectl describe certificate was-certificate -n was`
* Check for  `letsencrypt-cluster-issuer` clusterissuer with `kubectl describe clusterissuer letsencrypt-cluster-issuer`

---
