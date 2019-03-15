====
vssh
====

:Author: Stephane Martin

.. contents::
   :depth: 1
..

.. section-numbering::

introduction
============

``vssh`` is an SSH client that uses Hashicorp's vault to authenticate using SSH
certificates.

How it works:

-  first of all you need to configure the SSH certificate authority in Vault
   (see `Vault
   documentation <https://www.vaultproject.io/docs/secrets/ssh/signed-ssh-certificates.html>`__)

   -  inject the CA private and public keys into Vault
   -  configure the target OpenSSH servers to accept keys signed by the CA

-  just as with an usual SSH client, specify to vssh

   -  which server you want to connect to
   -  witch which remote user
   -  witch private key to use

-  say how to to connect to Vault

   -  Vault address
   -  Vault authentication (token, login/password, ...)

-  say which SSH signing role to use in Vault

vssh will then

-  submit your SSH private key to vault for signing
-  fetch the signed SSH certificate from vault
-  use the private key and the certificate to authenticate and connect to the
   remote SSH server

vssh can open an interactive shell on the remote server, or execute a command.

command line options
====================

Most of command line options can be specified with environnemt variables
instead. Check ``vssh --help`` for details.

SSH connection
--------------

-  **--sshuser** ``myuser``: connect to remote SSH server with user myuser
-  **--sshhost** ``myhost``: connect to remote SSH hist myhost
-  **--sshport** ``22``: SSH server remote port
-  **--insecure**: if specified, do not check SSH known hosts
-  **--native**: if specified, use the native ssh binary instead of the embedded
   Go SSH client

private key
-----------

-  **--privkey** ``id_rsa``: use the given private key to be signed by vault

vault connection
----------------

-  **--vault-addr** ``http://127.0.0.1:8200``: vault connection URL
-  **--method** ``userpass``: vault authentication method (token, userpass,
   ldap, approle)
-  **--token** ``tok``: which token to use with token auth
-  **--path** ``custompath``: useful if the Vault authentication method is
   mounted to a custom path
-  **--username** ``myvaultuser``: username for userpass auth
-  **--password** ``myvaultpass``: password for userpass auth

vault signing role
------------------

-  **--role** ``myrole``: the name of the SSH signing role you have configured
   in Vault

Examples
========

Questions
=========
