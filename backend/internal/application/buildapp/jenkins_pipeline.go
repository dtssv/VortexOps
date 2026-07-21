package buildapp

// pipelineJobConfigXML 返回 Jenkins 参数化 Pipeline job 的 config.xml。
// EnsureJob 创建 job 时使用此配置；参数与 triggerBuildEngineAsync 传入的 params 对齐。
//
// 传统 CI 模式 Pipeline 流程：
//   1. Checkout（git clone）到 Jenkins workspace。
//   2. Build：用 docker run BUILDER_IMAGE 执行 BUILD_COMMAND 产出制品（在共享卷上）。
//      builder_image 由 vo_build_tools 配置（maven/gradle/node/go...），Jenkins 无需装工具链。
//   3. Build & Push Image：用单阶段运行时 Dockerfile（只 COPY 制品）构建并 push。
//      - template 模式：DOCKERFILE 参数为渲染后的完整内容，写入文件后构建。
//      - repo 模式：DOCKERFILE 为空，用仓库自带 Dockerfile（DOCKERFILE_PATH）。
//   BUILD_ARGS（JSON）透传为 docker build --build-arg。
//
// workspace 共享：Jenkins 与 builder（docker daemon 所在容器）均挂载 buildworkspace 卷到
// /vortexops/workspace，docker run -v 与 docker build -f 的路径在 builder 侧可见。
// docker 命令通过环境变量 DOCKER_HOST=tcp://builder:2375 路由到独立 builder 容器。
const pipelineJobConfigXML = `<?xml version='1.1' encoding='UTF-8'?>
<flow-definition plugin="workflow-job">
  <description>VortexOps auto-generated build pipeline (traditional CI)</description>
  <keepDependencies>false</keepDependencies>
  <properties>
    <hudson.model.ParametersDefinitionProperty>
      <parameterDefinitions>
        <hudson.model.StringParameterDefinition>
          <name>REPO_URL</name>
          <description>Git repository URL</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>REF_VALUE</name>
          <description>Git branch or tag</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>COMMIT_SHA</name>
          <description>Expected commit SHA</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>IMAGE_REGISTRY</name>
          <description>Target registry host</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>IMAGE_REPO</name>
          <description>Target image repository</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>IMAGE_TAG</name>
          <description>Target image tag</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>BUILD_COMMAND</name>
          <description>Build command executed inside builder image</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>BUILDER_IMAGE</name>
          <description>Toolchain image to run BUILD_COMMAND (e.g. maven:3.9-eclipse-temurin-17)</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>ARTIFACT_PATH</name>
          <description>Artifact path produced by BUILD_COMMAND (template mode)</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.StringParameterDefinition>
          <name>DOCKERFILE_PATH</name>
          <description>Dockerfile path in repo (repo mode)</description>
        </hudson.model.StringParameterDefinition>
        <hudson.model.TextParameterDefinition>
          <name>DOCKERFILE</name>
          <description>Rendered single-stage runtime Dockerfile content (template mode)</description>
        </hudson.model.TextParameterDefinition>
        <hudson.model.TextParameterDefinition>
          <name>BUILD_ARGS_JSON</name>
          <description>JSON object of docker build --build-arg key/values</description>
        </hudson.model.TextParameterDefinition>
      </parameterDefinitions>
    </hudson.model.ParametersDefinitionProperty>
  </properties>
  <definition class="org.jenkinsci.plugins.workflow.cps.CpsFlowDefinition" plugin="workflow-cps">
    <script><![CDATA[pipeline {
  agent any
  options { timeout(time: 30, unit: 'MINUTES') }
  environment {
    // Unique build dir on the shared volume; visible to both Jenkins and the builder (docker daemon).
    SHARED_DIR = "/vortexops/workspace/${env.BUILD_ID ?: env.JOB_NAME}"
    // IMAGE_REGISTRY may carry a scheme (e.g. http://registry:5000); strip it for the image reference.
    REGISTRY_HOST = "${params.IMAGE_REGISTRY}".replaceFirst('^[a-zA-Z]+://', '')
    IMAGE_REF  = "${env.REGISTRY_HOST}/${params.IMAGE_REPO}:${params.IMAGE_TAG}"
    // Build stage runs BUILDER_IMAGE as root inside the builder container; artifacts (e.g. target/*.jar)
    // are owned by root and cannot be removed by Jenkins (uid 1000). CLEAN_CONTAINER is a throwaway
    // root container used to wipe SHARED_DIR, avoiding Permission denied residue that breaks the next Checkout.
    CLEAN_CONTAINER = "busybox"
  }
  stages {
    stage('Checkout') {
      steps {
        git branch: params.REF_VALUE, url: params.REPO_URL
        script {
          env.RESOLVED_SHA = sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
          echo "Resolved commit: ${env.RESOLVED_SHA}"
          // Prepare shared dir and sync source so builder-side docker run/build can access it.
          // Wipe leftover artifacts from the previous build via a root container (they are root-owned).
          // find -mindepth 1 -delete empties the dir (including hidden files) but keeps the dir itself, exit code 0.
          sh "docker run --rm -v ${env.SHARED_DIR}:/workspace ${env.CLEAN_CONTAINER} find /workspace -mindepth 1 -delete 2>/dev/null || true"
          // Ensure Jenkins(uid 1000) owns SHARED_DIR: if the dir was just created by the builder dockerd
          // (root) on first mount, Jenkins cannot cp into it. chown via the same root container.
          sh "docker run --rm -v ${env.SHARED_DIR}:/workspace ${env.CLEAN_CONTAINER} chown 1000:1000 /workspace 2>/dev/null || true"
          sh "mkdir -p ${env.SHARED_DIR}"
          sh "cp -a . ${env.SHARED_DIR}/"
        }
      }
    }
    stage('Build') {
      steps {
        script {
          // Traditional CI: run BUILD_COMMAND inside builder_image to produce artifacts.
          // Only run when both BUILDER_IMAGE and BUILD_COMMAND are non-empty (custom/repo mode may have none).
          if (params.BUILDER_IMAGE != null && params.BUILDER_IMAGE.trim() != '' &&
              params.BUILD_COMMAND != null && params.BUILD_COMMAND.trim() != '') {
            // Write BUILD_COMMAND to a script file to avoid shell quoting issues when nesting
            // it inside the builder container's sh -c. Mount the whole source dir into builder.
            writeFile file: "${env.SHARED_DIR}/.vortexops_build.sh", text: "#!/bin/sh\nset -e\n${params.BUILD_COMMAND}\n"
            sh "docker run --rm -v ${env.SHARED_DIR}:/workspace -w /workspace ${params.BUILDER_IMAGE} sh /workspace/.vortexops_build.sh"
            echo "Build artifacts produced in ${env.SHARED_DIR}"
          } else {
            echo "Skip build stage: no builder_image/build_command"
          }
        }
      }
    }
    stage('Build & Push Image') {
      steps {
        script {
          // Parse BUILD_ARGS_JSON into --build-arg flags using core Groovy JsonSlurper (no plugin needed).
          def buildArgs = []
          if (params.BUILD_ARGS_JSON != null && params.BUILD_ARGS_JSON.trim() != '') {
            try {
              def parsed = new groovy.json.JsonSlurper().parseText(params.BUILD_ARGS_JSON)
              parsed.each { k, v -> buildArgs << "--build-arg ${k}=${v}" }
            } catch (Exception e) {
              echo "WARN: parse BUILD_ARGS_JSON failed: ${e.message}"
            }
          }
          def argsStr = buildArgs.join(' ')
          // template mode: DOCKERFILE non-empty, write rendered single-stage runtime Dockerfile then build.
          // repo mode: DOCKERFILE empty, use repo-provided Dockerfile (DOCKERFILE_PATH).
          if (params.DOCKERFILE != null && params.DOCKERFILE.trim() != '') {
            writeFile file: "${env.SHARED_DIR}/Dockerfile.generated", text: params.DOCKERFILE
            sh "docker build ${argsStr} -t ${env.IMAGE_REF} -f ${env.SHARED_DIR}/Dockerfile.generated ${env.SHARED_DIR}"
          } else {
            def dfPath = (params.DOCKERFILE_PATH != null && params.DOCKERFILE_PATH.trim() != '') ? params.DOCKERFILE_PATH : 'Dockerfile'
            sh "docker build ${argsStr} -t ${env.IMAGE_REF} -f ${env.SHARED_DIR}/${dfPath} ${env.SHARED_DIR}"
          }
          sh "docker push ${env.IMAGE_REF}"
        }
      }
    }
  }
  post {
    failure {
      echo "Build failed"
    }
    success {
      echo "Build succeeded, image pushed to ${env.IMAGE_REF}"
    }
    always {
      script {
        // Clean up shared dir to avoid disk bloat.
        // Artifacts are root-owned (produced inside the builder container); Jenkins (uid 1000) cannot rm
        // them directly, so wipe via a throwaway root container.
        sh "docker run --rm -v ${env.SHARED_DIR}:/workspace ${env.CLEAN_CONTAINER} find /workspace -mindepth 1 -delete 2>/dev/null || true"
      }
    }
  }
}]]></script>
    <sandbox>false</sandbox>
  </definition>
  <triggers/>
  <disabled>false</disabled>
</flow-definition>`
