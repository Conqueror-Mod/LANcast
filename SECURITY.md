# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem. Report it
privately using GitHub's private vulnerability reporting instead: on this
repository's **Security** tab, choose **Report a vulnerability**. Only the
maintainer can see the report, and the fix can be discussed and released before
anything is made public.

Please include the LANcast version (Settings › About), the platform, and the
steps that show the problem. A proof of concept is welcome but not required.

## Supported versions

Only the latest release gets security fixes. LANcast updates itself from
Settings › Server › General, and a fix ships as a new release rather than as a patch to an
older one.

## What LANcast protects, and what it does not

[docs/security.md](docs/security.md) is the full threat model. It covers what
the server defends against, what it deliberately does not, and how to expose it
to the internet safely. Read it before assuming something is a vulnerability.
A behaviour it documents as out of scope is still worth reporting if you think
the reasoning is wrong.
