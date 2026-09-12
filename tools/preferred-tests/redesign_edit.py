from pathlib import Path
root=Path(__file__).resolve().parents[2]
p=root/'app/src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupActivity.kt'
s=p.read_text()
s=s.replace('import android.widget.*','import android.widget.*\nimport android.view.ViewGroup\nimport androidx.recyclerview.widget.RecyclerView\nimport androidx.recyclerview.widget.LinearLayoutManager')
s=s.replace('    private var profiles = emptyList<ProxyEntity>()\n','')
s=s.replace('                    profiles = SagerDatabase.proxyDao.getAll()','                    PreferredGroupStore.migrateLegacy()')
s=s.replace('requireNotNull(profiles.find { it.id == id })','requireNotNull(SagerDatabase.proxyDao.getById(id))')
s=s.replace('editing?.groupId ?: DataStore.selectedGroupForImport()','editing?.groupId ?: 0L')
s=s.replace('text = "保存位置：${groups.find { it.id == targetGroupId }?.displayName() ?: targetGroupId}（本地分组）"','text = "保存为首页独立分组，与订阅分组平级；原节点保留在来源分组。"')
a=s.index('    private fun chooseMembers() {'); b=s.index('    private fun chooseSources()',a)
s=s[:a]+'''    private fun chooseMembers() {
        val chosen = members.toMutableSet()
        val box = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val back = Button(this).apply { text = "返回分组"; isEnabled = false }
        val list = RecyclerView(this).apply { layoutManager = LinearLayoutManager(this@PreferredGroupActivity) }
        box.addView(back)
        box.addView(list, LinearLayout.LayoutParams(-1, (420 * resources.displayMetrics.density).toInt()))
        val dialog = MaterialAlertDialogBuilder(this).setTitle("按分组选择节点")
            .setView(box).setPositiveButton("确定") { _, _ -> members.clear(); members.addAll(chosen); updateCounts() }
            .setNegativeButton("取消", null).create()
        var generation = 0
        fun showNodes(group: ProxyGroup?) {
            val ticket = ++generation
            back.isEnabled = true
            list.adapter = null
            lifecycleScope.launch {
                try {
                    val nodes = withContext(Dispatchers.IO) {
                        if (group == null) chosen.mapNotNull { SagerDatabase.proxyDao.getById(it) }
                        else SagerDatabase.proxyDao.getByGroup(group.id)
                    }
                    if (!dialog.isShowing || ticket != generation) return@launch
                    list.adapter = object : RecyclerView.Adapter<ChoiceHolder>() {
                        override fun getItemCount() = nodes.size
                        override fun onCreateViewHolder(parent: ViewGroup, type: Int) = ChoiceHolder(CheckBox(parent.context))
                        override fun onBindViewHolder(holder: ChoiceHolder, position: Int) {
                            val node = nodes[position]
                            val check = holder.label as CheckBox
                            check.setOnCheckedChangeListener(null)
                            check.text = node.displayName() + if (node.configBean?.type == 2) "（不能嵌套优选）" else ""
                            check.isChecked = node.id in chosen
                            check.isEnabled = node.configBean?.type != 2 || check.isChecked
                            check.setOnCheckedChangeListener { _, checked ->
                                if (checked) chosen.add(node.id) else chosen.remove(node.id)
                            }
                        }
                    }
                } catch (e: CancellationException) { throw e }
                catch (e: Exception) { errorDialog(e.message ?: "读取分组失败") }
            }
        }
        fun showGroups() {
            generation++
            back.isEnabled = false
            val available = groups.filter { it.type != GroupType.PREFERRED }
            list.adapter = object : RecyclerView.Adapter<ChoiceHolder>() {
                override fun getItemCount() = available.size + 2
                override fun onCreateViewHolder(parent: ViewGroup, type: Int) = ChoiceHolder(Button(parent.context))
                override fun onBindViewHolder(holder: ChoiceHolder, position: Int) {
                    holder.label.text = when (position) {
                        0 -> "查看已选节点（${chosen.size}）"
                        1 -> "清空已选（包括失效引用）"
                        else -> available[position - 2].displayName() + "  ›"
                    }
                    holder.label.setOnClickListener {
                        when (position) {
                            0 -> showNodes(null)
                            1 -> { chosen.clear(); showGroups() }
                            else -> showNodes(available[position - 2])
                        }
                    }
                }
            }
        }
        back.setOnClickListener { showGroups() }
        dialog.setOnDismissListener { generation++ }
        dialog.show()
        showGroups()
    }

    private class ChoiceHolder(val label: TextView) : RecyclerView.ViewHolder(label) {
        init { label.layoutParams = RecyclerView.LayoutParams(-1, -2) }
    }
''' +s[b:]
s=s.replace('(groups.map { it.id } + sources)', '(groups.filter { it.type != GroupType.PREFERRED }.map { it.id } + sources)')
s=s.replace('(id == targetGroupId || groups.none { it.id == id })','(id == targetGroupId || groups.none { it.id == id && it.type != GroupType.PREFERRED })')
a=s.index('                    val all = SagerDatabase.proxyDao.getAll()');b=s.index('\n                }.let { persisted ->',a)
s=s[:a]+'''                    PreferredGroupStore.save(bean, editing).also { persisted ->
                        DataStore.selectedGroup = persisted.groupId
                        GroupManager.iterator { groupUpdated(persisted.groupId) }
                        ProfileManager.postUpdate(persisted)
                    }'''+s[b:]
p.write_text(s)
p=root/'app/src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt'
s=p.read_text().replace('                var newGroupList = ArrayList(SagerDatabase.groupDao.allGroups())','                PreferredGroupStore.migrateLegacy()\n                var newGroupList = ArrayList(SagerDatabase.groupDao.allGroups())')
s=s.replace('            return GroupFragment().apply {','            if (groupList[position].type == GroupType.PREFERRED) {\n                return PreferredGroupFragment.newInstance(groupList[position].id, select)\n            }\n            return GroupFragment().apply {')
s=s.replace('        override suspend fun groupUpdated(groupId: Long) = Unit','        override suspend fun groupUpdated(groupId: Long) { onMainDispatcher { reload() } }',1)
p.write_text(s)
