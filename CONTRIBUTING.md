# Contributing to Gorouter

The Cloud Foundry team uses GitHub and accepts contributions via [pull
request](https://help.github.com/articles/using-pull-requests).

## Contributor License Agreement

Follow these steps to make a contribution to any of our open source
repositories:

1. Ensure that you have completed our [CLA Agreement](https://cla.pivotal.io/)
   for [individuals](https://cla.pivotal.io/sign/pivotal/icla) or
   [corporations](https://cla.pivotal.io/sign/pivotal/ccla).

1. Set your name and email (these should match the information on your submitted
   CLA)

        git config --global user.name "Firstname Lastname" git config --global
        user.email "your_email@example.com"

## General Workflow

1. Fork the repository
1. Create a feature branch (`git checkout -b better_gorouter`)
1. Make changes on your branch
1. [Run tests](https://github.com/cloudfoundry/gorouter#running-tests)
1. Push to your fork (`git push origin better_gorouter`) and submit a pull
   request

We favor pull requests with very small, single commits with a single purpose.

Your pull request is much more likely to be accepted if:

* Your pull request includes tests
* Your pull request is small and focused with a clear message that conveys the
  intent of your change
