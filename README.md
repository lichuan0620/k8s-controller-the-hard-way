# K8S Controller 编写指南 - 受苦版

这篇指南将会教会你如何以正确的姿势编写 controller。本指南不适合着急快速写完一个能用就行的 controller 的读者。如果你确实有这样的需求，我建议直接学习 [kubebuilder](https://book.kubebuilder.io/)，或照抄 [sample-controller](https://github.com/kubernetes/sample-controller) 或任意一款热门 operator，保留大体框架的同时改掉业务逻辑即可（记得挑 Apache 2.0 执照的）。

这篇指南将带你从零开始编写一个 Redis controller。我们将会实现两个版本。在第一个版本中，我们会尽可能地不用 k8s client-go 之外的开源库地完成 Redis controller 的开发。实现这个版本的目的是了解一个 controller 的基本组成元件，了解它们分别扮演什么角色，是如何工作和交互的。在第二个版本中，我们将引入一些 [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime)（正文会解释）的原子能力以及笔者自己做的一些封装；在过程中我们将学习如何应对大规模场景，让 controller 更生产可用。


