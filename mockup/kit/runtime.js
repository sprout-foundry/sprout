(function(){
  // Link resolution (pure: no DOM access). Given a link href, its text and the
  // screen ids, decide what the click means.
  function slugify(text){
    return String(text||'').trim().toLowerCase().replace(/[^a-z0-9]+/g,'-').replace(/^-+|-+$/g,'');
  }
  // The href's last path segment without extension, query or fragment:
  // '/menu' → 'menu', 'menu.html' → 'menu', './menu?x=1' → 'menu', '#menu' → 'menu'.
  // Scheme hrefs (mailto:, tel:, https:, ...) are not page-like and return ''.
  function hrefSlug(href){
    var h=String(href||'').trim();
    if(/^[a-z][a-z0-9+.-]*:/i.test(h))return '';
    var frag='';
    var i=h.indexOf('#');
    if(i>=0){frag=h.slice(i+1);h=h.slice(0,i);}
    if(!h)h=frag;
    else{var j=h.lastIndexOf('/');if(j>=0)h=h.slice(j+1);}
    i=h.indexOf('?');if(i>=0)h=h.slice(0,i);
    h=h.toLowerCase();
    var dot=h.lastIndexOf('.');if(dot>0)h=h.slice(0,dot);
    return h;
  }
  // Find the screen id a link resolves to, or '' for "not a screen".
  // A link with a plain page href matches the screen whose id equals (or
  // equals the tail of) the slug of its href; a link with an unknown href
  // falls back to the slug of its text.
  function resolveLink(href,text,screens){
    var list=Array.prototype.slice.call(screens||[]);
    var h=hrefSlug(href);
    if(h){
      for(var i=0;i<list.length;i++){if(list[i].toLowerCase()===h)return list[i];}
    }
    var s=slugify(text);
    if(s){
      for(var j=0;j<list.length;j++){var id=list[j].toLowerCase();if(s===id||s.slice(-(id.length+1))==='-'+id)return list[j];}
    }
    return '';
  }
  // What an unresolved link gets: data-go-none, and the note to show.
  function noneKind(href){
    var h=String(href||'').trim().toLowerCase();
    if(h.indexOf('mailto:')===0)return 'mail';
    if(h.indexOf('tel:')===0)return 'tel';
    return 'none';
  }
  var NONE_NOTE={mail:'This opens email',tel:'This calls the number',none:"This link isn't part of the first look"};
  function exportInternals(){
    if(typeof module!=='undefined'&&module.exports)module.exports={slugify:slugify,hrefSlug:hrefSlug,resolveLink:resolveLink,noneKind:noneKind,NONE_NOTE:NONE_NOTE};
  }
  exportInternals();
  if(typeof window==='undefined'||typeof document==='undefined'||!document.body)return;
  function start(){
  document.body.classList.add('screens-ready');
  // Screens: one [data-screen] shown at a time; anything with data-go="id" shows that screen.
  function show(id){
    var screens=document.querySelectorAll('[data-screen]');if(!screens.length)return;
    var found=false;screens.forEach(function(s){var on=s.getAttribute('data-screen')===id;s.classList.toggle('is-shown',on);if(on)s.hidden=false;if(on)found=true;});
    if(!found){screens[0].classList.add('is-shown');screens[0].hidden=false;}
    document.querySelectorAll('[data-go]').forEach(function(a){a.classList.toggle('active',a.getAttribute('data-go')===id);});
    window.scrollTo(0,0);
  }
  // Links without data-go become screen moves: '#id' with an element is left
  // alone (in-page scroll); page-like hrefs match a screen id, else the text
  // slug, else they get data-go-none and a note instead of a navigation.
  var screenIds=Array.prototype.map.call(document.querySelectorAll('[data-screen]'),function(s){return s.getAttribute('data-screen')||'';});
  document.querySelectorAll('a[href]').forEach(function(a){
    if(a.hasAttribute('data-go'))return;
    var href=a.getAttribute('href')||'';
    if(href.charAt(0)==='#'){
      var id=href.slice(1);
      if(id&&document.getElementById(id))return; // in-page scroll, allowed
    }
    var text=a.textContent||'';
    var screen=resolveLink(href,text,screenIds);
    if(screen){a.setAttribute('data-go',screen);return;}
    a.setAttribute('data-go-none','');
    a.setAttribute('data-go-none-kind',noneKind(href));
  });
  // A note near the bottom of the viewport for about two seconds.
  var noteTimer=null;
  function showNote(msg){
    var old=document.querySelector('.go-note');if(old)old.remove();
    var note=document.createElement('div');note.className='go-note';note.setAttribute('role','status');note.textContent=msg;
    document.body.appendChild(note);
    if(noteTimer)clearTimeout(noteTimer);
    noteTimer=setTimeout(function(){note.remove();noteTimer=null;},2000);
  }
  var first=document.querySelector('[data-screen]');if(first)show(first.getAttribute('data-screen'));
  document.addEventListener('click',function(e){
    var go=e.target.closest&&e.target.closest('[data-go]');
    if(go){e.preventDefault();show(go.getAttribute('data-go'));return;}
    var none=e.target.closest&&e.target.closest('[data-go-none]');
    if(none){e.preventDefault();var kind=none.getAttribute('data-go-none-kind')||'none';showNote(NONE_NOTE[kind]||NONE_NOTE.none);return;}
    var tab=e.target.closest&&e.target.closest('[data-tabs] [data-tab]');
    if(tab){var box=tab.closest('[data-tabs]');var id=tab.getAttribute('data-tab');
      box.querySelectorAll('[data-tab]').forEach(function(t){t.setAttribute('aria-selected',t===tab?'true':'false');});
      box.querySelectorAll('[data-panel]').forEach(function(p){p.hidden=p.getAttribute('data-panel')!==id;});}
    var tog=e.target.closest&&e.target.closest('[data-toggle]');
    if(tog){var el=document.getElementById(tog.getAttribute('data-toggle'));if(el)el.hidden=!el.hidden;}
  });
  // Forms: submitting shows the form's .form-done message instead of sending anything.
  document.addEventListener('submit',function(e){var f=e.target;if(!(f instanceof HTMLFormElement))return;e.preventDefault();f.classList.add('is-done');f.querySelectorAll('.form-done').forEach(function(d){d.hidden=false;});});
  // Tabs start on their first tab.
  document.querySelectorAll('[data-tabs]').forEach(function(box){var t=box.querySelector('[data-tab]');if(t)t.click();});
  // Last guard: nothing navigates away — any anchor whose href is not an
  // in-document '#id' (including links added later by a look's own script)
  // has its default prevented in the capture phase. Legitimate in-page
  // scrolls to an existing element id are left alone.
  document.addEventListener('click',function(e){
    var a=e.target.closest&&e.target.closest('a[href]');
    if(!a)return;
    var href=a.getAttribute('href')||'';
    if(href.charAt(0)==='#'){var id=href.slice(1);if(id&&document.getElementById(id))return;}
    e.preventDefault();
  },true);
  }
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',start);else start();
  // Content: the host sends {type:'sprout-content', fields:{key:text}} to change text in place.
  window.addEventListener('message',function(e){var d=e.data;if(!d||d.type!=='sprout-content'||!d.fields)return;
    Object.keys(d.fields).forEach(function(k){var t=String(d.fields[k]);
      document.querySelectorAll('[data-c="'+k+'"]').forEach(function(el){var put=false;
        Array.prototype.slice.call(el.childNodes).forEach(function(n){var words=(n.textContent||'').trim()!=='';
          if(!words)return;
          if(!put&&n.nodeType===3){n.nodeValue=t;put=true;return;}
          if(!put){el.insertBefore(document.createTextNode(t),n);put=true;}
          el.removeChild(n);});
        if(!put)el.appendChild(document.createTextNode(t));});
      document.querySelectorAll('[data-c-ph="'+k+'"]').forEach(function(el){el.setAttribute('placeholder',t);});});});
})();
