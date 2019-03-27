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

``vssh`` is a SSH client that uses Hashicorp's vault to authenticate with SSH
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

install
=======

compile
=======

The dependencies are vondored using `dep <https://golang.github.io/dep/>`__. You
do not need it to compile vssh. Just clone in an appropriate directoty (GOROOT)
and run ``make release``.

.. code:: bash

   mdkir -p ~/go/src/github.com/stephane-martin
   cd ~/go/src/github.com/stephane-martin
   git clone https://github.com/stephane-martin/vssh
   cd vssh
   make release

develop
=======

-  Clone the repository in an appropriate go directory.
-  Install `dep <https://golang.github.io/dep/>`__ and
   `golangci-lint <https://github.com/golangci/golangci-lint>`__.
-  Compile with ``make debug`` and lint with ``make lint``.

usage
=====

vssh can open an interactive SSH session (``vssh ssh``), execute a remote
command (``vssh ssh``), and download (``vssh download``) / upload
(``vssh upload``) files using the scp protocol.

Most of command line options can be specified with environnemt variables
instead. Check ``vssh --help`` for details.

global options
--------------

The global options are useful for the different vssh commands. They configure
the connection to Vault.

+--------------+--------------------+-----------------------------------------+
| **Global     | **Value Example**  | **Definition**                          |
| option**     |                    |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-ad | ``http://127.0.0.1 | vault connection URL                    |
| dr``         | :8200``            |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-me | ``userpass``       | vault authentication method [token,     |
| thod``       |                    | userpass, ldap, approle]                |
+--------------+--------------------+-----------------------------------------+
| ``--vault-us | ``myvaultuser``    | username for vault authentication       |
| ername``     |                    |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-pa | ``myvaultpass``    | password for vault authentication       |
| ssword``     |                    |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-ss | ``myrole``         | name of the SSH sign role you have      |
| h-role``     |                    | configured in Vault                     |
+--------------+--------------------+-----------------------------------------+
| ``--vault-to | ``s.lIz3muuaUOZe42 | token for vault authentication          |
| ken``        | 4j2ZI5GTDK``       |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-ss | ``ssh-client-signe | the path to the SSH signer in Vault     |
| h-mount``    | r``                |                                         |
+--------------+--------------------+-----------------------------------------+
| ``--vault-au | ``custompath``     | if the Vault authentication method is   |
| th-path``    |                    | mounted to a custom path                |
+--------------+--------------------+-----------------------------------------+

interactive SSH session
-----------------------

.. code:: bash

   vssh [global options] ssh [ssh options] user@host

   vssh ssh --help

vssh needs a private key to send to Vault for signature. You can give it:

-  a private key that is stored locally on your filesystem with ``--identity``
-  or a private key stored in vault with ``--videntity``

vssh will ask for a passphrase if the private key is stored in encrypted form.

+------------+---------------------+-------------------------------------------+
| **SSH      | **Value Example**   | **Definition**                            |
| option**   |                     |                                           |
+------------+---------------------+-------------------------------------------+
| ``--identi | ``/path/to/id_rsa`` | file path to the SSH private key that     |
| ty``       |                     | should be signed                          |
+------------+---------------------+-------------------------------------------+
| ``--vident | ``secret/id_rsa_in_ | Vault path to the SSH private key that    |
| ity``      | vault``             | should be signed                          |
+------------+---------------------+-------------------------------------------+
| ``--insecu |                     | do not check the SSH server host key      |
| re``       |                     |                                           |
+------------+---------------------+-------------------------------------------+
| ``--native |                     | use the local ``ssh`` binary to make the  |
| ``         |                     | connection                                |
+------------+---------------------+-------------------------------------------+
| ``--termin |                     | force pseudo-terminal allocation          |
| al``       |                     |                                           |
+------------+---------------------+-------------------------------------------+
| ``--ssh-po | ``22``              | SSH server listen port                    |
| rt``       |                     |                                           |
+------------+---------------------+-------------------------------------------+
| ``--login` | ``admin``           | alternate way to specify the remote user  |
| `          |                     |                                           |
+------------+---------------------+-------------------------------------------+

remote command
--------------

.. code:: bash

   vssh [global options] ssh [ssh options] user@host command

   vssh [global options] ssh -t [ssh options] user@host command

Just put the command the execute at the end of the ``vssh ssh`` command line.

If the command is meant to be interactive, then you need to add the ``-t`` flag.
For example, to launch an alternate shell:

.. code:: bash

   vssh ssh -t me@remote zsh

It is also possible to inject some Vault secrets into the remote command
environment, similarly to ``--envconsul``, with the following flags:

+------------+---------------+------------------------------------------------+
| **SSH      | **Value       | **Definition**                                 |
| option**   | Example**     |                                                |
+------------+---------------+------------------------------------------------+
| ``--secret | ``secret/path | path of a secret to read from Vault            |
| ``         | ``            |                                                |
+------------+---------------+------------------------------------------------+
| ``--upcase |               | convert environment variable keys to UPPERCASE |
| ``         |               |                                                |
+------------+---------------+------------------------------------------------+
| ``--prefix |               | prefix the environment variable keys with      |
| ``         |               | names of secrets                               |
+------------+---------------+------------------------------------------------+

download
--------

.. code:: bash

   vssh [global options] download [download options] --target file1 [--target file2...] user@host

   vssh download --help

Specify the remote files/directories you want to download with the ``--target``
flag. It can appear multiple times.

Specify the local destination path with the ``--destination`` flag.

The other flags are similar to the ``vssh ssh`` command.

+---------------+--------------------+-----------------------------------------+
| **download    | **Value Example**  | **Definition**                          |
| option**      |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--identity` | ``/path/to/id_rsa` | file path to the SSH private key that   |
| `             | `                  | should be signed                        |
+---------------+--------------------+-----------------------------------------+
| ``--videntity | ``secret/id_rsa_in | Vault path to the SSH private key that  |
| ``            | _vault``           | should be signed                        |
+---------------+--------------------+-----------------------------------------+
| ``--insecure` |                    | do not check the SSH server host key    |
| `             |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--target``  | ``remotefile``     | path to the remote file to be           |
|               |                    | downloaded                              |
+---------------+--------------------+-----------------------------------------+
| ``--destinati | ``/tmp``           | local destination path                  |
| on``          |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--ssh-port` | ``22``             | SSH server listen port                  |
| `             |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--login``   | ``admin``          | alternate way to specify the remote     |
|               |                    | user                                    |
+---------------+--------------------+-----------------------------------------+
| ``--preserve` |                    | preserve file mode, access time and     |
| `             |                    | modification time                       |
+---------------+--------------------+-----------------------------------------+

upload
------

.. code:: bash

   vssh [global options] upload [upload options] user@host

   vssh upload --help

Specify the local files/directories you want to upload with the ``--source``
flag. It can appear multiple times.

Specify the remote destination path with the ``--destination`` flag.

The other flags are similar to the ``vssh ssh`` command.

+---------------+--------------------+-----------------------------------------+
| **download    | **Value Example**  | **Definition**                          |
| option**      |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--identity` | ``/path/to/id_rsa` | file path to the SSH private key that   |
| `             | `                  | should be signed                        |
+---------------+--------------------+-----------------------------------------+
| ``--videntity | ``secret/id_rsa_in | Vault path to the SSH private key that  |
| ``            | _vault``           | should be signed                        |
+---------------+--------------------+-----------------------------------------+
| ``--insecure` |                    | do not check the SSH server host key    |
| `             |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--source``  | ``localfile``      | path to the local file to be uploaded   |
+---------------+--------------------+-----------------------------------------+
| ``--destinati | ``/tmp``           | remote destination path                 |
| on``          |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--ssh-port` | ``22``             | SSH server listen port                  |
| `             |                    |                                         |
+---------------+--------------------+-----------------------------------------+
| ``--login``   | ``admin``          | alternate way to specify the remote     |
|               |                    | user                                    |
+---------------+--------------------+-----------------------------------------+

as a library
------------

TODO

examples
========

Let's assume you have configured a few environment variables, to avoid
repetition in the examples.

.. code:: bash

   export VAULT_ADDR=https://vault.example.org:8200
   export VAULT_SSH_MOUNT=ssh-client-signer
   export VAULT_SIGNING_ROLE=my-vault-ssh-role

With such variables, vssh knowns:

-  how to connect to the Vault server instance
-  which certificate authority to use in Vault
-  which SSH role to use in Vault to produce the certificates

Let's also assume you have generated a SSH private key for your local current
user:

::

   ssh-keygen

single sign on
--------------

Open a terminal, then authenticate yourself with Vault:

.. code:: bash

   vault login -method=userpass username=bob

The ``vault login`` command writes the resulting token in ``~/.vault_token``. If
you don't specify to vssh how to authenticate to Vault, by default it will use
that token.

You can then SSH to any server that recognizes the Vault CA:

.. code:: bash

   vssh ssh me@myserver.example.org

execute a remote command
------------------------

.. code:: bash

   vssh ssh me@myserver.example.org ls -al / 

execute a remote command in a pseudo-terminal
---------------------------------------------

.. code:: bash

   vssh ssh -t me@myserver.example.org zsh

inject Vault secrets in the remote session
------------------------------------------

Now let's say you want to execute a remote command on a server, but some part of
the configuration for that command is stored in Vault.

``vssh`` can work similar to ``envconsul``:

.. code:: bash

   vssh ssh --secret secret/mysecret me@myserver.example.org backupcommand

**Locally**, ``vssh`` will read the required secret from Vault. Then it opens
the SSH connection. Then the command will be executed, with environment
variables corresponding to the secrets.

So, if ``secret/mysecret`` is something like:

::

   foo=bar
   ZOG=ZOG

then vssh executes on the remote SSH server:

.. code:: bash

   env foo=bar ZOG=ZOG backupcommand

with the additional ``--upcase`` flag, it becomes:

.. code:: bash

   env FOO=bar ZOG=ZOG backupcommand

or with the additional ``--prefix`` flag it becomes:

.. code:: bash

   env secret_mysecret_foo=bar secret_mysecret_ZOG=ZOG backupcommand

Your remote SSH environment doesn't have to know anything about Vault by itself.

questions
=========

what does the ``--native`` flag do ?
------------------------------------

By default vssh uses an internal SSH client implemented in Go.

-  Go implementation, so vssh does not need to launch another process.
-  Might behave differently compared to the native ssh command.
-  Does not read ``.ssh/config``.
-  The signed certificate is not written to the filesystem, it is passed
   directly to the SSH client in memory.

With ``--native``, vssh wraps the native ``ssh`` binary. It can be useful it you
wish to enable the native configuration of the SSH client
(``man 5 ssh_config``).

-  there vssh launches a SSH subprocess
-  the SSH subprocess will read ssh_config as usual
-  to pass the signed certificate to SSH, vssh has to write it to the filesystem
   (it will be removed at the end of execution)

what should be the TTL for signed certificates ?
------------------------------------------------

Very short. After Vault has signed the SSH certificate, vssh uses that
certificate immediatly and only once. Every time vssh is executed, another
certificate will be created. So in theory, a TTL of a few seconds is just
enough.
