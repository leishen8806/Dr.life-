
const menu=document.querySelector('.menu-toggle'),nav=document.querySelector('#site-nav');
function closeMenu(){nav.dataset.open='false';menu.setAttribute('aria-expanded','false')}
menu.addEventListener('click',()=>{const open=nav.dataset.open!=='true';nav.dataset.open=String(open);menu.setAttribute('aria-expanded',String(open))});
nav.addEventListener('click',e=>{if(e.target.closest('a')) closeMenu()});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&nav.dataset.open==='true'){closeMenu();menu.focus()}});
const partnerData=[{"key":"buyers","name":"消费者与装修团队","en":"BUYERS & PROJECTS","desc":"选设计、选施工、选材料、看样品，对接采购、安装及平台监理与验收。","action":"准备采购需求","fields":["所需品类、数量或项目面积","预算范围与期望交付日期","看样、采购、配送或安装需求","设计、施工、平台监理与验收需求"]},{"key":"brands","name":"中国品牌与工厂","en":"BRANDS & FACTORIES","desc":"了解一仓入柬、商户入驻和本地展示，规划商品在柬埔寨的销售与交付。","action":"准备入柬资料","fields":["品牌、产品目录及主推品类","起订量、包装规格及预计发货量","希望对接的运输、展厅或销售方式"]},{"key":"merchants","name":"本地商户与服务伙伴","en":"LOCAL MERCHANTS","desc":"洽谈平台入驻、线下展厅，以及装修和安装团队的服务合作。","action":"准备合作资料","fields":["企业或团队名称及经营范围","主营品类、服务区域及以往项目","拟合作方式：入驻、展厅或本地服务"]},{"key":"fulfillment","name":"平台及仓配客户","en":"FULFILLMENT CLIENTS","desc":"按订单量、货物类型和交付范围，洽谈独立仓配服务。","action":"准备仓配需求","fields":["货品类别、库存体积及订单量","配送范围、大件搬运与安装要求","期望时效、退换货及系统对接需求"]},{"key":"finance","name":"商户金融咨询","en":"BUSINESS FINANCE","desc":"沟通备货、采购或订单履约相关的经营资金需求。","action":"准备金融咨询资料","fields":["经营主体与主营业务","拟融资的用途及期望期限","可说明经营情况的基础资料（请通过正式渠道提交）"]}];
const dialog=document.querySelector('#inquiry'), fieldList=document.querySelector('#dialog-fields'), brief=document.querySelector('#inquiry-text'), status=document.querySelector('#copy-status');
let opener=null;
document.querySelectorAll('[data-inquiry]').forEach(btn=>btn.addEventListener('click',()=>{
 const data=partnerData.find(p=>p.key===btn.dataset.inquiry);if(!data)return;
 opener=btn;
 document.querySelector('#dialog-title').textContent=data.action;
 fieldList.replaceChildren(...data.fields.map(v=>{const li=document.createElement('li');li.textContent=v;return li}));
 brief.value=['您好，我想咨询 DR.LIFE+ 的「'+data.name+'」。',...data.fields.map((v,i)=>(i+1)+'、'+v+'：')].join('\n');
 status.textContent='';
 dialog.showModal();
}));
dialog.addEventListener('close',()=>opener?.focus());
dialog.addEventListener('click',e=>{if(e.target===dialog)dialog.close()});
document.querySelector('#copy-inquiry').addEventListener('click',async()=>{
 try{
  if(!navigator.clipboard?.writeText)throw Error();
  await navigator.clipboard.writeText(brief.value);
  status.textContent='已复制咨询模板。';
 }catch{
  brief.focus();brief.select();
  status.textContent=document.execCommand('copy')?'已复制咨询模板。':'已选中模板，请复制。';
 }
});

// The source design stays structurally identical in every language.  Only text
// nodes and accessible labels are translated here, so the layout cannot drift
// between the zh/en/km screenshot matrix.
const translations={
  en:{
    '跳至主要内容':'Skip to main content','菜单':'Menu','四大版块':'Four businesses','DR智慧建材城':'DR Smart Materials City','一仓入柬':'One warehouse into Cambodia','关于DR':'About DR','App 下载':'Download the app','合作咨询':'Work with DR','立足金边，连接全球供应链与柬埔寨生活。':'Rooted in Phnom Penh, connecting global supply chains with life in Cambodia.','探索四大版块':'Explore four businesses','洽谈合作':'Discuss a partnership','从选材到交付，':'From material selection to delivery,','每一步都有 DR。':'DR is with you at every step.','消费者与装修团队':'Buyers & projects','中国品牌与工厂':'Brands & factories','本地商户与服务伙伴':'Local merchants','平台及仓配客户':'Fulfillment clients','金融服务只与持牌机构合作，尚未开放。':'Financial services will work only with licensed institutions and are not yet available.','准备金融咨询资料':'Prepare a finance enquiry','选材，先看实物。':'See materials in person before choosing.','准备看样及采购需求':'Prepare a showroom and purchasing enquiry','准备供应商入柬资料':'Prepare supplier information','相信生活有更多可能！':'Believe in more possibilities for life!','与 DR.LIFE+ 一起成长':'Grow with DR.LIFE+','下载 App，关注 DR':'Download the apps and follow DR','准备咨询资料':'Prepare enquiry details','关闭':'Close','复制咨询模板':'Copy enquiry template'},
  km:{
    '跳至主要内容':'រំលងទៅមាតិកាសំខាន់','菜单':'ម៉ឺនុយ','四大版块':'អាជីវកម្ម ៤ ផ្នែក','DR智慧建材城':'មជ្ឈមណ្ឌលសម្ភារៈសំណង់ DR','一仓入柬':'ឃ្លាំងមួយចូលកម្ពុជា','关于DR':'អំពី DR','App 下载':'ទាញយកកម្មវិធី','合作咨询':'ពិភាក្សាកិច្ចសហការ','立足金边，连接全球供应链与柬埔寨生活。':'មានមូលដ្ឋាននៅភ្នំពេញ និងភ្ជាប់ខ្សែសង្វាក់ផ្គត់ផ្គង់ជាមួយជីវិតនៅកម្ពុជា។','探索四大版块':'ស្វែងរកអាជីវកម្ម ៤ ផ្នែក','洽谈合作':'ពិភាក្សាកិច្ចសហការ','金融服务只与持牌机构合作，尚未开放。':'សេវាហិរញ្ញវត្ថុ នឹងសហការតែជាមួយស្ថាប័នមានអាជ្ញាប័ណ្ណ ហើយមិនទាន់បើកដំណើរការទេ។','准备金融咨询资料':'រៀបចំព័ត៌មានសម្រាប់សំណួរហិរញ្ញវត្ថុ','选材，先看实物。':'ជ្រើសរើសសម្ភារៈ ដោយមើលវត្ថុពិតជាមុន។','准备看样及采购需求':'រៀបចំព័ត៌មានសម្រាប់មើលគំរូ និងទិញ','准备供应商入柬资料':'រៀបចំព័ត៌មានអ្នកផ្គត់ផ្គង់','相信生活有更多可能！':'ជឿថាជីវិតមានលទ្ធភាពច្រើនជាងនេះ!','与 DR.LIFE+ 一起成长':'រីកចម្រើនជាមួយ DR.LIFE+','下载 App，关注 DR':'ទាញយកកម្មវិធី និងតាមដាន DR','准备咨询资料':'រៀបចំព័ត៌មានសម្រាប់សំណួរ','关闭':'បិទ','复制咨询模板':'ចម្លងគំរូសំណួរ'}
};
const language=new URLSearchParams(location.search).get('lang')||document.body.dataset.lang||'zh';
const dictionary=translations[language];
document.documentElement.lang=language==='zh'?'zh-CN':language==='km'?'km-KH':'en';
document.body.dataset.lang=language;
if(dictionary){
  const walker=document.createTreeWalker(document.body,NodeFilter.SHOW_TEXT);
  const nodes=[]; while(walker.nextNode()) nodes.push(walker.currentNode);
  for(const node of nodes){const value=node.nodeValue.trim(); if(dictionary[value]) node.nodeValue=node.nodeValue.replace(value,dictionary[value]);}
  document.querySelectorAll('[alt],[aria-label]').forEach((node)=>{for(const attr of ['alt','aria-label']) if(dictionary[node.getAttribute(attr)]) node.setAttribute(attr,dictionary[node.getAttribute(attr)]);});
}

const channelStatus=document.querySelector('#channel-status');
document.querySelectorAll('[data-copy-channel]').forEach(btn=>btn.addEventListener('click',async()=>{
 let copied=false;
 try { if(!navigator.clipboard?.writeText) throw Error(); await navigator.clipboard.writeText('DR520'); copied=true; }
 catch {
  const box=document.createElement('textarea');box.value='DR520';box.readOnly=true;
  box.style.position='fixed';box.style.left='-9999px';document.body.append(box);
  box.select();try { copied=document.execCommand('copy'); } catch {}
  box.remove();btn.focus();
 }
 channelStatus.textContent=copied?'已复制 '+btn.dataset.copyChannel+' 账号：DR520。':'账号：DR520，请手动复制。';
}));

