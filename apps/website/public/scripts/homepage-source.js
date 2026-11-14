
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
    '从供应商的实际需要出发，减少重复中转与交接，让好产品出海更简单高效。':'Start from supplier needs, reduce repeated handoffs, and make cross-border sales simpler and more efficient.','沟通备货、采购或订单履约相关的经营资金需求。':'Discuss working capital needs for stocking, purchasing, and order fulfilment.','经营主体与主营业务':'Business entity and core activity','拟融资的用途及期望期限':'Financing purpose and desired term','可说明经营情况的基础资料（请通过正式渠道提交）':'Basic business information (please submit through official channels)',
    '全屋定制':'Whole-home custom','家具软装':'Furniture and soft furnishings','生活家电':'Home appliances','电动工具':'Power tools','日用百货':'Daily goods',
    '瓷砖':'Tiles','卫浴':'Sanitaryware','岩板':'Sintered stone','石材':'Stone','门窗':'Doors and windows','灯具':'Lighting','五金':'Hardware','板材':'Panels','油漆涂料':'Paint and coatings',
    '瓷砖 · 卫浴 · 岩板 · 石材 · 门窗 · 灯具 · 五金 · 板材 · 油漆涂料':'Tiles · sanitaryware · sintered stone · stone · doors · lighting · hardware · panels · paint','全屋定制 · 家具软装 · 生活家电':'Whole-home custom · furniture · appliances','电动工具 · 日用百货':'Power tools · daily goods',
    '金融服务':'Financial services','面向有经营周转需求的商户，了解申请条件与所需材料。':'For merchants seeking working capital, learn about eligibility and required materials.','供应链金融':'Supply-chain finance','结合采购、库存与订单等经营场景，沟通相应融资需求。':'Discuss financing needs around procurement, inventory and orders.','DR.LIFE+ 生活家将金边线下建材城纳入平台服务。展厅按品类规划，方便消费者、设计师和装修团队集中看样，比对颜色、材质和工艺，再对接采购与交付。':'DR.LIFE+ brings the Phnom Penh building-materials showroom into the platform, making it easier for consumers, designers and renovation teams to compare samples before procurement and delivery.',
    '跳至主要内容':'Skip to main content','菜单':'Menu','四大版块':'Four businesses','DR智慧建材城':'DR Smart Materials City','一仓入柬':'One warehouse into Cambodia','关于DR':'About DR','App 下载':'Download the app','合作咨询':'Work with DR','立足金边，连接全球供应链与柬埔寨生活。':'Rooted in Phnom Penh, connecting global supply chains with life in Cambodia.','探索四大版块':'Explore four businesses','洽谈合作':'Discuss a partnership','从选材到交付，':'From material selection to delivery,','每一步都有 DR。':'DR is with you at every step.','消费者与装修团队':'Buyers & projects','中国品牌与工厂':'Brands & factories','本地商户与服务伙伴':'Local merchants','平台及仓配客户':'Fulfillment clients','金融服务只与持牌机构合作，尚未开放。':'Financial services will work only with licensed institutions and are not yet available.','准备金融咨询资料':'Prepare a finance enquiry','选材，先看实物。':'See materials in person before choosing.','准备看样及采购需求':'Prepare a showroom and purchasing enquiry','准备供应商入柬资料':'Prepare supplier information','相信生活有更多可能！':'Believe in more possibilities for life!','与 DR.LIFE+ 一起成长':'Grow with DR.LIFE+','下载 App，关注 DR':'Download the apps and follow DR','准备咨询资料':'Prepare enquiry details','关闭':'Close','复制咨询模板':'Copy enquiry template'},
  // [TODO-KM-REVIEW] Native-speaker review remains required before final publication.
  km:{
    '金融服务':'សេវាហិរញ្ញវត្ថុ','面向有经营周转需求的商户，了解申请条件与所需材料。':'សម្រាប់ពាណិជ្ជករដែលត្រូវការទុនបង្វិល ស្វែងយល់ពីលក្ខខណ្ឌ និងឯកសារតម្រូវការ។','供应链金融':'ហិរញ្ញវត្ថុខ្សែសង្វាក់ផ្គត់ផ្គង់','结合采购、库存与订单等经营场景，沟通相应融资需求。':'ពិភាក្សាតម្រូវការហិរញ្ញវត្ថុជុំវិញការទិញ ស្តុក និងការបញ្ជាទិញ។','DR.LIFE+ 生活家将金边线下建材城纳入平台服务。展厅按品类规划，方便消费者、设计师和装修团队集中看样，比对颜色、材质和工艺，再对接采购与交付。':'DR.LIFE+ នាំបន្ទប់តាំងបង្ហាញសម្ភារៈសំណង់ភ្នំពេញចូលក្នុងវេទិកា ដើម្បីឱ្យអ្នកប្រើ អ្នករចនា និងក្រុមជួសជុលប្រៀបធៀបគំរូ មុនពេលទិញ និងប្រគល់។',
    '从供应商的实际需要出发，减少重复中转与交接，让好产品出海更简单高效。':'ចាប់ផ្តើមពីតម្រូវការអ្នកផ្គត់ផ្គង់ កាត់បន្ថយការបញ្ជូនបន្ត និងធ្វើឱ្យការលក់ឆ្លងដែនមានប្រសិទ្ធភាព។','沟通备货、采购或订单履约相关的经营资金需求。':'ពិភាក្សាតម្រូវការទុនសម្រាប់ស្តុក ទិញ និងបំពេញការបញ្ជាទិញ។','经营主体与主营业务':'អង្គភាពអាជីវកម្ម និងសកម្មភាពសំខាន់','拟融资的用途及期望期限':'គោលបំណងហិរញ្ញវត្ថុ និងរយៈពេលដែលរំពឹង','可说明经营情况的基础资料（请通过正式渠道提交）':'ព័ត៌មានមូលដ្ឋានអាជីវកម្ម (សូមដាក់តាមឆានែលផ្លូវការ)',
    '全屋定制':'គេហដ្ឋានតាមតម្រូវការ','家具软装':'គ្រឿងសង្ហារឹម និងការតុបតែង','生活家电':'ឧបករណ៍ប្រើប្រាស់ក្នុងគេហដ្ឋាន','电动工具':'ឧបករណ៍អគ្គិសនី','日用百货':'ទំនិញប្រចាំថ្ងៃ',
    '瓷砖':'ក្បឿង','卫浴':'សម្ភារៈបន្ទប់ទឹក','岩板':'ថ្មសintered','石材':'ថ្ម','门窗':'ទ្វារ និងបង្អួច','灯具':'ភ្លើង','五金':'ឧបករណ៍','板材':'បន្ទះ','油漆涂料':'ថ្នាំលាប',
    '瓷砖 · 卫浴 · 岩板 · 石材 · 门窗 · 灯具 · 五金 · 板材 · 油漆涂料':'ក្បឿង · សម្ភារៈបន្ទប់ទឹក · ថ្មសintered · ថ្ម · ទ្វារ · ភ្លើង · ឧបករណ៍ · បន្ទះ · ថ្នាំលាប','全屋定制 · 家具软装 · 生活家电':'គេហដ្ឋានតាមតម្រូវការ · គ្រឿងសង្ហារឹម · ឧបករណ៍ប្រើប្រាស់','电动工具 · 日用百货':'ឧបករណ៍អគ្គិសនី · ទំនិញប្រចាំថ្ងៃ',
    '跳至主要内容':'រំលងទៅមាតិកាសំខាន់','菜单':'ម៉ឺនុយ','四大版块':'អាជីវកម្ម ៤ ផ្នែក','DR智慧建材城':'មជ្ឈមណ្ឌលសម្ភារៈសំណង់ DR','一仓入柬':'ឃ្លាំងមួយចូលកម្ពុជា','关于DR':'អំពី DR','App 下载':'ទាញយកកម្មវិធី','合作咨询':'ពិភាក្សាកិច្ចសហការ','立足金边，连接全球供应链与柬埔寨生活。':'មានមូលដ្ឋាននៅភ្នំពេញ និងភ្ជាប់ខ្សែសង្វាក់ផ្គត់ផ្គង់ជាមួយជីវិតនៅកម្ពុជា។','探索四大版块':'ស្វែងរកអាជីវកម្ម ៤ ផ្នែក','洽谈合作':'ពិភាក្សាកិច្ចសហការ','金融服务只与持牌机构合作，尚未开放。':'សេវាហិរញ្ញវត្ថុ នឹងសហការតែជាមួយស្ថាប័នមានអាជ្ញាប័ណ្ណ ហើយមិនទាន់បើកដំណើរការទេ។','准备金融咨询资料':'រៀបចំព័ត៌មានសម្រាប់សំណួរហិរញ្ញវត្ថុ','选材，先看实物。':'ជ្រើសរើសសម្ភារៈ ដោយមើលវត្ថុពិតជាមុន។','准备看样及采购需求':'រៀបចំព័ត៌មានសម្រាប់មើលគំរូ និងទិញ','准备供应商入柬资料':'រៀបចំព័ត៌មានអ្នកផ្គត់ផ្គង់','相信生活有更多可能！':'ជឿថាជីវិតមានលទ្ធភាពច្រើនជាងនេះ!','与 DR.LIFE+ 一起成长':'រីកចម្រើនជាមួយ DR.LIFE+','下载 App，关注 DR':'ទាញយកកម្មវិធី និងតាមដាន DR','准备咨询资料':'រៀបចំព័ត៌មានសម្រាប់សំណួរ','关闭':'បិទ','复制咨询模板':'ចម្លងគំរូសំណួរ'}
};
const requestedLanguage=new URLSearchParams(location.search).get('lang');
const language=['zh','en','km'].includes(requestedLanguage)?requestedLanguage:(document.body.dataset.lang||'zh');
const dictionary=translations[language];
const homepageCopyElement=document.querySelector('#homepage-copy');
if(dictionary&&homepageCopyElement){
  const homepageCopy=JSON.parse(homepageCopyElement.textContent||'{}');
  const flatten=(value,path=[],result={})=>{
    if(typeof value==='string'){result[path.join('.')]=value;return result;}
    if(Array.isArray(value)){value.forEach((item,index)=>flatten(item,[...path,index],result));return result;}
    if(value&&typeof value==='object') Object.entries(value).forEach(([key,item])=>flatten(item,[...path,key],result));
    return result;
  };
  const source=flatten(homepageCopy.zh);
  const target=flatten(homepageCopy[language]);
  Object.entries(source).forEach(([path,value])=>{if(typeof value==='string'&&target[path]&&/[\u4e00-\u9fff]/.test(value)) dictionary[value]=target[path];});
}
const extraTranslations={
  en:{
    'DR集团':'DR Group','生活·':'Life ·','无限可能':'Infinite possibilities','从建材家居选购到本地配送安装，DR.LIFE+ 将线上平台、DR智慧建材城、跨境物流、云仓配送与商户金融服务连接起来。':'From building materials and home selection to local delivery and installation, DR.LIFE+ connects commerce, showroom, cross-border logistics, warehousing and merchant services.','App 下载二维码':'App QR codes','金边建材家居空间 · 概念示意':'Phnom Penh building-materials and home space · concept','DR.LIFE+ 生活家':'DR.LIFE+ Life & Home','DR跨境物流':'DR Cross-border Logistics','DR云仓及智能配送':'DR Warehousing & Delivery','DR金融':'DR Finance','家居展厅 · 场景示意':'Home showroom · scene','线上选购，线下看样':'Shop online, see materials in person','把中国货源接到柬埔寨':'Bring Chinese supply to Cambodia','跨境物流 · 场景示意':'Cross-border logistics · scene','智慧云仓 · 场景示意':'Smart warehouse · scene','仓里有序，交付有据':'Orderly storage, accountable delivery','商户经营 · 场景示意':'Merchant operations · scene','让商户周转更从容':'Give merchants more breathing room','DR智慧建材城 · 展厅场景示意':'DR Smart Building-Materials City · showroom scene','具体服务主体、产品、适用对象、费率和审批条件，以正式披露为准。':'Service entity, product, eligibility, rates and approval terms follow formal disclosures.','展厅为一期规划，具体品类与开放安排以公告为准。':'The showroom is planned for phase one; categories and opening arrangements follow formal announcements.','金边 · 柬埔寨':'Phnom Penh · Cambodia','集团文化':'DR culture','App与官方渠道':'Apps and official channels','© DR集团 · DR.LIFE+':'© DR Group · DR.LIFE+','准备咨询资料':'Prepare enquiry details','关闭':'Close','可先准备以下信息，再与 DR 对接具体需求。':'Prepare the following information before discussing your needs with DR.','复制':'Copy','微信公众号':'WeChat Official Account','DR App 下载':'DR app downloads','DR.LIFE+ App 下载':'DR.LIFE+ app download','建材家居选购与装修服务':'Building materials, home shopping and renovation','DR仓配 App 下载':'DR fulfilment app download','云仓及同城配送服务':'Warehousing and local delivery','DR金融 App 下载':'DR Finance app download','金融服务咨询':'Financial services enquiry','演示':'Demo','演示二维码：扫码可查看示范信息，正式 App 下载链接待发布。':'Demo QR code: scan to view sample information; official app links will be published later.','官方渠道':'Official channels','在对应平台搜索账号':'Search for the account on the relevant platform','，了解 DR 最新动态。':', for the latest DR updates.','准备采购需求':'Prepare procurement details','准备入柬资料':'Prepare Cambodia-entry details','准备合作资料':'Prepare cooperation details','准备仓配需求':'Prepare fulfilment details','准备金融咨询资料':'Prepare finance enquiry details','了解金边智慧建材城':'Explore the Phnom Penh showroom','了解一仓入柬':'Explore one warehouse into Cambodia','了解仓配合作':'Explore fulfilment cooperation','金融服务暂未开放':'Finance not yet available','中国端':'China side','柬埔寨端':'Cambodia side','一仓入柬':'One warehouse into Cambodia','本地销售与交付':'Local sales and delivery','中国集货':'China consolidation','跨境运输':'Cross-border transport','金边入仓':'Phnom Penh inbound','从生活需要出发':'Start with real life','让好产品以更低成本入柬':'Make good products easier to bring in','与伙伴长期合作':'Build for the long term','商户金融咨询':'Merchant finance consultation','消费者与装修团队':'Consumers and project teams','中国品牌与工厂':'Chinese brands and factories','本地商户与服务伙伴':'Local merchants and service partners','平台及仓配客户':'Platform and fulfilment clients','企业或团队名称及经营范围':'Business or team name and scope','主营品类、服务区域及以往项目':'Main categories, service area and previous projects','拟合作方式：入驻、展厅或本地服务':'Proposed cooperation: onboarding, showroom or local service','货品类别、库存体积及订单量':'Goods category, inventory volume and order volume','配送范围、大件搬运与安装要求':'Delivery area, large-item handling and installation requirements','期望时效、退换货及系统对接需求':'Expected timing, returns and system integration requirements','DR金融 App':'DR Finance App'},
  km:{
    'DR集团':'ក្រុម DR','生活·':'ជីវិត ·','无限可能':'លទ្ធភាពគ្មានដែនកំណត់','从建材家居选购到本地配送安装，DR.LIFE+ 将线上平台、DR智慧建材城、跨境物流、云仓配送与商户金融服务连接起来。':'ពីការជ្រើសរើសសម្ភារៈសំណង់ និងគេហដ្ឋាន ដល់ការដឹកជញ្ជូន និងដំឡើងក្នុងស្រុក DR.LIFE+ ភ្ជាប់ពាណិជ្ជកម្ម បន្ទប់តាំងបង្ហាញ ការដឹកជញ្ជូនឆ្លងដែន ឃ្លាំង និងសេវាពាណិជ្ជករ។','App 下载二维码':'QR កូដកម្មវិធី','金边建材家居空间 · 概念示意':'លំហសម្ភារៈសំណង់ និងគេហដ្ឋានភ្នំពេញ · គំរូ','DR.LIFE+ 生活家':'DR.LIFE+ ជីវិត និងគេហដ្ឋាន','DR跨境物流':'DR ដឹកជញ្ជូនឆ្លងដែន','DR云仓及智能配送':'DR ឃ្លាំង និងដឹកជញ្ជូន','DR金融':'DR ហិរញ្ញវត្ថុ','家居展厅 · 场景示意':'បន្ទប់តាំងបង្ហាញគេហដ្ឋាន · ឈុត','线上选购，线下看样':'ទិញតាមអនឡាញ មើលគំរូផ្ទាល់','把中国货源接到柬埔寨':'នាំប្រភពផ្គត់ផ្គង់ចិនមកកម្ពុជា','跨境物流 · 场景示意':'ដឹកជញ្ជូនឆ្លងដែន · ឈុត','智慧云仓 · 场景示意':'ឃ្លាំងឆ្លាតវៃ · ឈុត','仓里有序，交付有据':'ឃ្លាំងមានរបៀបរៀបរយ ការប្រគល់អាចតាមដាន','商户经营 · 场景示意':'ប្រតិបត្តិការពាណិជ្ជករ · ឈុត','让商户周转更从容':'ធ្វើឱ្យការបង្វិលទុនពាណិជ្ជករកាន់តែងាយ','DR智慧建材城 · 展厅场景示意':'ទីក្រុងសម្ភារៈ DR · ឈុតបន្ទប់តាំងបង្ហាញ','具体服务主体、产品、适用对象、费率和审批条件，以正式披露为准。':'អង្គភាពសេវា ផលិតផល អ្នកមានសិទ្ធិ អត្រា និងលក្ខខណ្ឌ តាមការបង្ហាញផ្លូវការ។','展厅为一期规划，具体品类与开放安排以公告为准。':'បន្ទប់តាំងបង្ហាញគឺជាផែនការដំណាក់កាលទី១។ ប្រភេទទំនិញ និងការបើកដំណើរការតាមការប្រកាស។','金边 · 柬埔寨':'ភ្នំពេញ · កម្ពុជា','集团文化':'ស្មារតី DR','App与官方渠道':'កម្មវិធី និងឆានែលផ្លូវការ','© DR集团 · DR.LIFE+':'© ក្រុម DR · DR.LIFE+','准备咨询资料':'រៀបចំព័ត៌មានសម្រាប់សំណួរ','关闭':'បិទ','可先准备以下信息，再与 DR 对接具体需求。':'អាចរៀបចំព័ត៌មានខាងក្រោមជាមុន មុនពិភាក្សាតម្រូវការជាក់លាក់ជាមួយ DR។','复制':'ចម្លង','微信公众号':'គណនី WeChat ផ្លូវការ','DR App 下载':'ទាញយកកម្មវិធី DR','DR.LIFE+ App 下载':'ទាញយកកម្មវិធី DR.LIFE+','建材家居选购与装修服务':'សម្ភារៈសំណង់ គេហដ្ឋាន និងសេវាជួសជុល','DR仓配 App 下载':'ទាញយកកម្មវិធី DR ឃ្លាំង','云仓及同城配送服务':'ឃ្លាំង និងដឹកជញ្ជូនក្នុងក្រុង','DR金融 App 下载':'ទាញយកកម្មវិធី DR ហិរញ្ញវត្ថុ','金融服务咨询':'សំណួរសេវាហិរញ្ញវត្ថុ','演示':'គំរូ','演示二维码：扫码可查看示范信息，正式 App 下载链接待发布。':'QR កូដគំរូ៖ ស្កេនដើម្បីមើលព័ត៌មានគំរូ។ តំណទាញយកផ្លូវការនឹងប្រកាសនៅពេលក្រោយ។','官方渠道':'ឆានែលផ្លូវការ','在对应平台搜索账号':'ស្វែងរកគណនីលើវេទិកាដែលពាក់ព័ន្ធ','，了解 DR 最新动态。':' ដើម្បីទទួលព័ត៌មានថ្មីៗពី DR។','准备采购需求':'រៀបចំព័ត៌មានទិញ','准备入柬资料':'រៀបចំព័ត៌មានចូលកម្ពុជា','准备合作资料':'រៀបចំព័ត៌មានសហការ','准备仓配需求':'រៀបចំព័ត៌មានឃ្លាំង និងដឹកជញ្ជូន','准备金融咨询资料':'រៀបចំព័ត៌មានសំណួរហិរញ្ញវត្ថុ','了解金边智慧建材城':'ស្វែងយល់អំពីបន្ទប់តាំងបង្ហាញភ្នំពេញ','了解一仓入柬':'ស្វែងយល់អំពីឃ្លាំងមួយទៅកម្ពុជា','了解仓配合作':'ស្វែងយល់អំពីសហការឃ្លាំង និងដឹកជញ្ជូន','金融服务暂未开放':'ហិរញ្ញវត្ថុមិនទាន់បើក','中国端':'ភាគីចិន','柬埔寨端':'ភាគីកម្ពុជា','一仓入柬':'ឃ្លាំងមួយទៅកម្ពុជា','本地销售与交付':'លក់ និងប្រគល់ក្នុងស្រុក','中国集货':'ប្រមូលទំនិញនៅចិន','跨境运输':'ដឹកជញ្ជូនឆ្លងដែន','金边入仓':'ចូលឃ្លាំងភ្នំពេញ','从生活需要出发':'ចាប់ផ្តើមពីជីវិតពិត','让好产品以更低成本入柬':'ធ្វើឱ្យផលិតផលល្អចូលកម្ពុជាងាយជាងមុន','与伙伴长期合作':'សាងសង់សម្រាប់រយៈពេលវែង','商户金融咨询':'ការប្រឹក្សាហិរញ្ញវត្ថុពាណិជ្ជករ','消费者与装修团队':'អ្នកប្រើ និងក្រុមគម្រោង','中国品牌与工厂':'ម៉ាក និងរោងចក្រចិន','本地商户与服务伙伴':'ពាណិជ្ជករ និងដៃគូសេវាក្នុងស្រុក','平台及仓配客户':'អតិថិជនវេទិកា និងឃ្លាំង','企业或团队名称及经营范围':'ឈ្មោះអាជីវកម្ម ឬក្រុម និងវិសាលភាព','主营品类、服务区域及以往项目':'ប្រភេទសំខាន់ៗ តំបន់សេវា និងគម្រោងកន្លងមក','拟合作方式：入驻、展厅或本地服务':'វិធីសហការដែលស្នើ៖ ចូលវេទិកា បន្ទប់តាំងបង្ហាញ ឬសេវាក្នុងស្រុក','货品类别、库存体积及订单量':'ប្រភេទទំនិញ បរិមាណស្តុក និងចំនួនបញ្ជាទិញ','配送范围、大件搬运与安装要求':'តំបន់ប្រគល់ និងតម្រូវការលើកដាក់ទំនិញធំ និងដំឡើង','期望时效、退换货及系统对接需求':'ពេលវេលាដែលរំពឹង ការប្តូរទំនិញ និងតម្រូវការភ្ជាប់ប្រព័ន្ធ','DR金融 App':'DR ហិរញ្ញវត្ថុ App'}
};
if(dictionary) Object.assign(dictionary,extraTranslations[language]||{});
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

