
document.getElementById('q').addEventListener('input', function () {
  var needle = this.value.toLowerCase();
  document.querySelectorAll('details.row').forEach(function (row) {
    row.style.display = !needle ||
      row.textContent.toLowerCase().indexOf(needle) !== -1 ? '' : 'none';
  });
});
