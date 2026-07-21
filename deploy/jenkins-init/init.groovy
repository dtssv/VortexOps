// VortexOps Jenkins 初始化脚本（init.groovy.d）
// 自动创建 admin 用户、禁用安装向导、创建 vortexops job folder。
// 首次启动 Jenkins 时执行。

import jenkins.model.*
import hudson.security.*
import com.cloudbees.plugins.credentials.*
import com.cloudbees.plugins.credentials.domains.*
import com.cloudbees.plugins.credentials.impl.*

def instance = Jenkins.getInstanceOrNull()
if (instance == null) {
    return
}

// ---------- 1. 禁用安装向导 ----------
def setupWizard = instance.getSetupWizard()
if (setupWizard != null) {
    setupWizard.completeSetup()
}

// ---------- 2. 创建/更新 admin 用户 ----------
def hudsonRealm = instance.getSecurityRealm()
if (hudsonRealm instanceof HudsonPrivateSecurityRealm) {
    def adminUser = hudsonRealm.getUser("admin")
    if (adminUser == null) {
        adminUser = hudsonRealm.createAccount("admin", System.getenv("JENKINS_ADMIN_PASSWORD") ?: "vortexops_dev")
    } else {
        adminUser.setPassword(System.getenv("JENKINS_ADMIN_PASSWORD") ?: "vortexops_dev")
    }
}

// ---------- 3. vortexops job folder ----------
// 不在 init.groovy 创建 folder：EnsureJob 会在首次触发构建时通过 REST API 自动创建。
// 早期版本曾在此用反射创建 Folder，但 Folder 插件版本间构造函数签名不一致，
// 触发 NoSuchMethodException 导致 Jenkins 启动失败。folder 交由后端 EnsureJob 兜底。
println "[VortexOps] Jenkins folder creation deferred to EnsureJob (backend)."

instance.save()
println "[VortexOps] Jenkins init complete."
